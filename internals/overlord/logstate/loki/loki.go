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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/pebble/internals/httputil"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	requestTimeout    = 10 * time.Second
	maxRequestEntries = 100
)

type Client struct {
	options    *ClientOptions
	httpClient *http.Client

	// To store log entries, keep a buffer of size 2*MaxRequestEntries with a
	// sliding window 'entries' of size MaxRequestEntries
	buffer  []entryWithService
	entries []entryWithService

	// store the custom labels for each service
	labels map[string]json.RawMessage
}

func NewClient(options *ClientOptions) (*Client, error) {
	opts := *options
	fillDefaultOptions(&opts)

	// Validate URL in FIPS builds
	if err := httputil.ValidateURL(opts.Location); err != nil {
		return nil, fmt.Errorf("invalid location URL for target %q: %w", opts.TargetName, err)
	}

	c := &Client{
		options:    &opts,
		httpClient: httputil.NewClient(httputil.ClientOptions{Timeout: opts.RequestTimeout}),
		buffer:     make([]entryWithService, 2*opts.MaxRequestEntries),
		labels:     make(map[string]json.RawMessage),
	}
	// c.entries should be backed by the same array as c.buffer
	c.entries = c.buffer[:0]
	return c, nil
}

// ClientOptions allows overriding default parameters (e.g. for testing)
type ClientOptions struct {
	RequestTimeout    time.Duration
	MaxRequestEntries int
	UserAgent         string
	TargetName        string
	Location          string
}

func fillDefaultOptions(options *ClientOptions) {
	if options.RequestTimeout == 0 {
		options.RequestTimeout = requestTimeout
	}
	if options.MaxRequestEntries == 0 {
		options.MaxRequestEntries = maxRequestEntries
	}
}

func (c *Client) SetLabels(serviceName string, labels map[string]string) {
	if labels == nil {
		delete(c.labels, serviceName)
		return
	}

	// Make a copy to avoid altering the original map
	newLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		newLabels[k] = v
	}

	// Add Loki-specific default labels
	newLabels["pebble_service"] = serviceName

	// Encode labels now to save time later
	marshalledLabels, err := json.Marshal(newLabels)
	if err != nil {
		// Can't happen as map[string]string will always be marshallable
		logger.Panicf("Loki client for %q: cannot marshal labels: %v", c.options.TargetName, err)
	}
	c.labels[serviceName] = marshalledLabels
}

func (c *Client) Add(entry servicelog.Entry) error {
	if len(c.entries) >= c.options.MaxRequestEntries {
		// 'entries' is full - remove the first element to make room
		// Zero the removed element to allow garbage collection
		c.entries[0] = entryWithService{}
		c.entries = c.entries[1:]
	}

	if len(c.entries) >= cap(c.entries) {
		// Copy all the elements to the start of the buffer
		copy(c.buffer, c.entries)

		// Reset the view into the buffer
		c.entries = c.buffer[:len(c.entries):len(c.buffer)]

		// Zero removed elements to allow garbage collection
		for i := len(c.entries); i < len(c.buffer); i++ {
			c.buffer[i] = entryWithService{}
		}
	}

	c.entries = append(c.entries, entryWithService{
		entry:   encodeEntry(entry),
		service: entry.Service,
	})
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
		return fmt.Errorf("cannot encode request to JSON: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.options.Location, bytes.NewReader(jsonReq))
	if err != nil {
		return fmt.Errorf("cannot create HTTP request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("User-Agent", c.options.UserAgent)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}

	return c.handleServerResponse(resp)
}

// resetBuffer drops all buffered logs (in the case of a successful send, or an
// unrecoverable error).
func (c *Client) resetBuffer() {
	// Zero removed elements to allow garbage collection
	for i := 0; i < len(c.entries); i++ {
		c.entries[i] = entryWithService{}
	}
	c.entries = c.buffer[:0]
}

func (c *Client) buildRequest() lokiRequest {
	// Put entries into service "buckets"
	bucketedEntries := map[string][]lokiEntry{}
	for _, data := range c.entries {
		bucketedEntries[data.service] = append(bucketedEntries[data.service], data.entry)
	}

	// Sort service names to guarantee deterministic output
	var services []string
	for service := range bucketedEntries {
		services = append(services, service)
	}
	sort.Strings(services)

	var req lokiRequest
	for _, service := range services {
		entries := bucketedEntries[service]
		stream := lokiStream{
			Labels:  c.labels[service],
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
	Labels  json.RawMessage `json:"stream"`
	Entries []lokiEntry     `json:"values"`
}

type lokiEntry [2]string

type entryWithService struct {
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
		// Other 4xx codes indicate a client problem, so drop the logs (retrying won't help)
		logger.Noticef("Target %q: request failed with status %d, dropping %d logs",
			c.options.TargetName, code, len(c.entries))
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
