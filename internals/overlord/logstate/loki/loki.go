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
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

const maxRequestEntries = 1000

var requestTimeout = 10 * time.Second

type Client struct {
	targetName string
	remoteURL  string
	// buffered entries are "sharded" by service name
	entries map[string][]lokiEntry
	// Keep track of the number of buffered entries, to avoid having to iterate
	// the entries map to get the total number.
	numEntries int

	httpClient *http.Client
	retryAfter *time.Time
}

func NewClient(target *plan.LogTarget) *Client {
	return &Client{
		targetName: target.Name,
		remoteURL:  target.Location,
		entries:    map[string][]lokiEntry{},
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) Write(ctx context.Context, entry servicelog.Entry) error {
	c.entries[entry.Service] = append(c.entries[entry.Service], encodeEntry(entry))
	c.numEntries++

	if c.numEntries >= maxRequestEntries {
		if c.retryAfter != nil {
			if time.Now().Before(*c.retryAfter) {
				// Retry-After deadline hasn't passed yet, so we shouldn't flush
				return nil
			}
			c.retryAfter = nil
		}
		return c.Flush(ctx)
	}
	return nil
}

func encodeEntry(entry servicelog.Entry) lokiEntry {
	return lokiEntry{
		strconv.FormatInt(entry.Time.UnixNano(), 10),
		strings.TrimSuffix(entry.Message, "\n"),
	}
}

func (c *Client) Flush(ctx context.Context) error {
	if c.numEntries == 0 {
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

	err, emptyBuffer := c.handleServerResponse(resp)
	if emptyBuffer {
		if err != nil {
			logger.Noticef("Target %q: request failed, dropping %d logs",
				c.targetName, c.numEntries)
		}
		c.emptyBuffer()
	}
	return err
}

func (c *Client) emptyBuffer() {
	for svc := range c.entries {
		c.entries[svc] = c.entries[svc][:0]
	}
	c.numEntries = 0
}

func (c *Client) buildRequest() request {
	// Sort keys to guarantee deterministic output
	services := make([]string, 0, len(c.entries))
	for svc, entries := range c.entries {
		if len(entries) == 0 {
			continue
		}
		services = append(services, svc)
	}
	sort.Strings(services)

	var req request
	for _, service := range services {
		entries := c.entries[service]
		stream := stream{
			Labels: map[string]string{
				"pebble_service": service,
			},
			Entries: entries,
		}
		req.Streams = append(req.Streams, stream)
	}
	return req
}

type request struct {
	Streams []stream `json:"streams"`
}

type stream struct {
	Labels  map[string]string `json:"stream"`
	Entries []lokiEntry       `json:"values"`
}

type lokiEntry [2]string

// handleServerResponse determines what to do based on the response from the
// Loki server. 4xx and 5xx responses indicate errors, so in this case, we will
// bubble up and error to the caller. The returned boolean indicates whether
// the buffer should be emptied.
func (c *Client) handleServerResponse(resp *http.Response) (err error, emptyBuffer bool) {
	defer func() {
		// Drain request body to allow connection reuse
		// see https://pkg.go.dev/net/http#Response.Body
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		_ = resp.Body.Close()
	}()

	code := resp.StatusCode
	switch {
	case code == http.StatusTooManyRequests:
		// For 429, don't drop logs - just retry later
		return errFromResponse(resp), false

	case code >= 500:
		// 5xx indicates a problem with the server, so don't drop logs
		return errFromResponse(resp), false

	case code >= 400:
		// 4xx indicates a client problem, so drop the logs
		return errFromResponse(resp), true
	}

	return nil, true
}

// errFromResponse generates an error from a failed *http.Response.
// NB: this function reads the response body.
func errFromResponse(resp *http.Response) error {
	// Request to Loki failed
	errStr := fmt.Sprintf("server returned HTTP %d\n", resp.StatusCode)

	// Read response body to get more context
	body := make([]byte, 0, 1024)
	_, err := resp.Body.Read(body)
	if err == nil {
		errStr += fmt.Sprintf(`response body:
%s
`, body)
	} else {
		errStr += "cannot read response body: " + err.Error()
	}

	return errors.New(errStr)
}
