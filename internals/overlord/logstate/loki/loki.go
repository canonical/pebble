// Copyright (c) 2023 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

const maxRequestEntries = 100

var requestTimeout = 10 * time.Second

type Client struct {
	targetName string
	remoteURL  string
	entries    []lokiEntryWithService

	httpClient *http.Client
}

func NewClient(target *plan.LogTarget) *Client {
	return &Client{
		targetName: target.Name,
		remoteURL:  target.Location,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) Add(entry servicelog.Entry) error {
	c.entries = append(c.entries, lokiEntryWithService{
		entry:   encodeEntry(entry),
		service: entry.Service,
	})
	c.truncateBuffer()
	return nil
}

func encodeEntry(entry servicelog.Entry) lokiEntry {
	return lokiEntry{
		strconv.FormatInt(entry.Time.UnixNano(), 10),
		strings.TrimSuffix(entry.Message, "\n"),
	}
}

func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil // no-op
	}

	req := c.buildRequest()
	jsonReq, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encoding request to JSON: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.remoteURL, bytes.NewReader(jsonReq))
	if err != nil {
		return fmt.Errorf("creating HTTP request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("User-Agent", fmt.Sprintf("pebble/%s", cmd.Version))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}

	return c.handleServerResponse(resp)
}

// resetBuffer drops all buffered logs (in the case of a successful send, or an
// unrecoverable error).
func (c *Client) resetBuffer() {
	c.entries = c.entries[:0]
}

// truncateBuffer truncates the buffer to maxRequestEntries, removing the
// oldest logs first.
func (c *Client) truncateBuffer() {
	numEntries := len(c.entries)
	if numEntries > maxRequestEntries {
		c.entries = c.entries[(numEntries - maxRequestEntries):]
	}
}

func (c *Client) buildRequest() lokiRequest {
	// Put entries into service "buckets"
	bucketedEntries := map[string][]lokiEntry{}
	for _, data := range c.entries {
		bucketedEntries[data.service] = append(bucketedEntries[data.service], data.entry)
	}

	var req lokiRequest
	for service, entries := range bucketedEntries {
		stream := lokiStream{
			Labels: map[string]string{
				"pebble_service": service,
			},
			Entries: entries,
		}
		req.Streams = append(req.Streams, stream)
	}
	return req
}

type lokiRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Labels  map[string]string `json:"stream"`
	Entries []lokiEntry       `json:"values"`
}

type lokiEntry [2]string

type lokiEntryWithService struct {
	entry   lokiEntry
	service string
}

// handleServerResponse determines what to do based on the response from the
// Loki server. 4xx and 5xx responses indicate errors, so in this case, we will
// bubble up the error to the caller.
func (c *Client) handleServerResponse(resp *http.Response) error {
	defer func() {
		// Drain request body to allow connection reuse
		// see https://pkg.go.dev/net/http#Response.Body
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		_ = resp.Body.Close()
	}()

	code := resp.StatusCode
	switch {
	case code == http.StatusOK || code == http.StatusNoContent:
		// Success - safe to drop logs
		c.resetBuffer()
		return nil

	case code == http.StatusTooManyRequests:
		// For 429, don't drop logs - just retry later
		return errFromResponse(resp)

	case 400 <= code && code < 500:
		// 4xx indicates a client problem, so drop the logs (retrying won't help)
		logger.Noticef("Target %q: request failed with status %d, dropping %d logs",
			c.targetName, code, len(c.entries))
		c.resetBuffer()
		return errFromResponse(resp)

	case 500 <= code && code < 600:
		// 5xx indicates a problem with the server, so don't drop logs (retry later)
		return errFromResponse(resp)

	default:
		// Unexpected response - don't drop logs to be safe
		return fmt.Errorf("unexpected response from server: %v", resp.Status)
	}
}

// errFromResponse generates an error from a failed *http.Response.
// Note: this function reads the response body.
func errFromResponse(resp *http.Response) error {
	// Read response body to get more context
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err == nil {
		logger.Debugf("HTTP %d error, response %q", resp.StatusCode, body)
	} else {
		logger.Debugf("HTTP %d error, but cannot read response: %v", resp.StatusCode, err)
	}

	return fmt.Errorf("server returned HTTP %v", resp.Status)
}
