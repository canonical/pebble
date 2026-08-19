// Copyright (c) 2025 Canonical Ltd
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

package opentelemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/canonical/pebble/internals/logger"
	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	logspb "github.com/canonical/pebble/internals/otlp/logs/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
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
	// sliding window "entries" of size MaxRequestEntries.
	buffer  []entryWithService
	entries []entryWithService

	// Store the custom labels for each service (resource attributes in OTEL).
	resourceAttributes map[string][]*commonpb.KeyValue
}

func NewClient(options *ClientOptions) *Client {
	opts := *options
	fillDefaultOptions(&opts)
	c := &Client{
		options:            &opts,
		httpClient:         &http.Client{Timeout: opts.RequestTimeout},
		buffer:             make([]entryWithService, 2*opts.MaxRequestEntries),
		resourceAttributes: make(map[string][]*commonpb.KeyValue),
	}
	// c.entries should be backed by the same array as c.buffer.
	c.entries = c.buffer[:0]
	return c
}

// ClientOptions allows overriding default parameters (e.g. for testing).
type ClientOptions struct {
	RequestTimeout    time.Duration
	MaxRequestEntries int
	UserAgent         string
	ScopeName         string
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

// SetLabels sets resource attributes for a service. Labels are analogous to OpenTelemetry's resource attributes.
func (c *Client) SetLabels(serviceName string, attributes map[string]string) {
	if attributes == nil {
		delete(c.resourceAttributes, serviceName)
		return
	}

	// Convert attributes to commonpb.KeyValue format.
	keyValuePairs := make([]*commonpb.KeyValue, 0, len(attributes)+1)

	// Add service.name attribute.
	keyValuePairs = append(keyValuePairs, &commonpb.KeyValue{
		Key:   "service.name",
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: serviceName}},
	})

	// Sort other labels to ensure deterministic order.
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := attributes[k]
		keyValuePairs = append(keyValuePairs, &commonpb.KeyValue{
			Key:   k,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}},
		})
	}

	c.resourceAttributes[serviceName] = keyValuePairs
}

func (c *Client) Add(entry servicelog.Entry) error {
	if len(c.entries) >= c.options.MaxRequestEntries {
		// "entries" is full - remove the first element to make room.
		// Zero the removed element to allow garbage collection.
		c.entries[0] = entryWithService{}
		c.entries = c.entries[1:]
	}

	if len(c.entries) >= cap(c.entries) {
		// Copy all the elements to the start of the buffer.
		copy(c.buffer, c.entries)

		// Reset the view into the buffer.
		c.entries = c.buffer[:len(c.entries):len(c.buffer)]

		// Zero removed elements to allow garbage collection.
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

func encodeEntry(entry servicelog.Entry) *logspb.LogRecord {
	message := strings.TrimSuffix(entry.Message, "\n")

	return &logspb.LogRecord{
		TimeUnixNano: uint64(entry.Time.UnixNano()),
		Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: message}},
	}
}

// Flush sends the buffered logs to the OpenTelemetry collector.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil // no-op
	}

	// Group entries by service.
	serviceBatches := make(map[string][]*logspb.LogRecord)
	for _, otelEntryWithService := range c.entries {
		serviceName := otelEntryWithService.service
		logRecord := otelEntryWithService.entry
		serviceBatches[serviceName] = append(serviceBatches[serviceName], logRecord)
	}

	serviceNames := make([]string, 0, len(serviceBatches))
	for serviceName := range serviceBatches {
		serviceNames = append(serviceNames, serviceName)
	}
	// Sort service names to ensure deterministic order.
	sort.Strings(serviceNames)

	logs := make([]*logspb.ResourceLogs, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		batch := serviceBatches[serviceName]

		resourceAttributes := c.resourceAttributes[serviceName]
		resource := &resourcepb.Resource{
			Attributes: resourceAttributes,
		}
		scope := &commonpb.InstrumentationScope{Name: c.options.ScopeName}
		scopeLogs := []*logspb.ScopeLogs{{
			Scope:      scope,
			LogRecords: batch,
		}}
		logs = append(logs, &logspb.ResourceLogs{
			Resource:  resource,
			ScopeLogs: scopeLogs,
		})
	}

	data := &logspb.LogsData{
		ResourceLogs: logs,
	}

	return c.sendBatch(ctx, data)
}

func (c *Client) sendBatch(ctx context.Context, data *logspb.LogsData) error {
	protoData, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("cannot marshal log batch: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.options.Location+"/v1/logs", bytes.NewReader(protoData))
	if err != nil {
		return fmt.Errorf("cannot create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("User-Agent", c.options.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot send logs: %v", err)
	}

	return c.handleServerResponse(resp)
}

// resetBuffer drops all buffered logs (in the case of a successful send, or an unrecoverable error).
func (c *Client) resetBuffer() {
	// Zero removed elements to allow garbage collection.
	for i := 0; i < len(c.entries); i++ {
		c.entries[i] = entryWithService{}
	}
	c.entries = c.buffer[:0]
}

type entryWithService struct {
	entry   *logspb.LogRecord
	service string
}

// handleServerResponse determines what to do based on the response from the
// OpenTelemetry collector. 4xx and 5xx responses indicate errors, so in this case, we will
// bubble up the error to the caller.
func (c *Client) handleServerResponse(resp *http.Response) error {
	defer func() {
		// Drain request body to allow connection reuse.
		// See https://pkg.go.dev/net/http#Response.Body
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		_ = resp.Body.Close()
	}()

	code := resp.StatusCode
	switch {
	case code == http.StatusOK || code == http.StatusNoContent:
		// Success - safe to drop logs.
		c.resetBuffer()
		return nil

	case code == http.StatusTooManyRequests:
		// For 429, don't drop logs - just retry later.
		return errFromResponse(resp)

	case 400 <= code && code < 500:
		// Other 4xx codes indicate a client problem, so drop the logs (retrying won't help).
		logger.Noticef("Target %q: request failed with status %d, dropping %d logs",
			c.options.TargetName, code, len(c.entries))
		c.resetBuffer()
		return errFromResponse(resp)

	case 500 <= code && code < 600:
		// 5xx indicates a problem with the server, so don't drop logs (retry later).
		return errFromResponse(resp)

	default:
		// Unexpected response - don't drop logs to be safe.
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
