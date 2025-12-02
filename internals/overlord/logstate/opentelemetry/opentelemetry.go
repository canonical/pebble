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

// A collection of ScopeLogs from a Resource.
// Refer to `type ResourceLogs struct` in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/logs/v1/logs.pb.go#L223
type resourceLogs struct {
	// The resource for the logs in this message.
	// If this field is not set then resource info is unknown.
	Resource resource `json:"resource"`
	// A list of ScopeLogs that originate from a resource.
	ScopeLogs []scopeLogs `json:"scopeLogs,omitempty"`
}

// Resource information, partially from 'type Resource struct' in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/resource/v1/resource.pb.go#L30
type resource struct {
	Attributes []keyValue `json:"attributes"`
}

// keyValue is a key-value pair that stores attributes.
// from 'type KeyValue struct' in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/common/v1/common.pb.go#L286
type keyValue struct {
	Key   string   `json:"key,omitempty"`
	Value anyValue `json:"value"`
}

// anyValue represents the OTLP attribute value format.
// Refer to `type AnyValue struct` in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/common/v1/common.pb.go#L31
type anyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
}

// A collection of Logs produced by a Scope.
// Refer to `type ScopeLogs struct` in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/logs/v1/logs.pb.go#L301
type scopeLogs struct {
	// The instrumentation scope information for the logs in this message.
	// Semantically when InstrumentationScope isn't set, it is equivalent with
	// an empty instrumentation scope name (unknown).
	Scope instrumentationScope `json:"scope"`
	// A list of log records.
	LogRecords []logRecord `json:"logRecords,omitempty"`
}

// instrumentationScope is a message representing the instrumentation scope information
// such as the fully qualified name and version.
// Refer to `type InstrumentationScope struct` in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/common/v1/common.pb.go#L340
type instrumentationScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// A log record according to OpenTelemetry Log Data Model:
// https://github.com/open-telemetry/oteps/blob/main/text/logs/0097-log-data-model.md
// Refer to `type LogRecord struct` in
// https://github.com/open-telemetry/opentelemetry-collector/blob/3c0fd3946f70a0b1fa97813c39dbc4d91d95afa6/pdata/internal/data/protogen/logs/v1/logs.pb.go#L372
type logRecord struct {
	// time_unix_nano is the time when the event occurred.
	// Value is UNIX Epoch time in nanoseconds since 00:00:00 UTC on 1 January 1970.
	// Value of 0 indicates unknown or missing timestamp.
	TimeUnixNano string `json:"timeUnixNano"`
	// Numerical value of the severity, normalized to values described in Log Data Model.
	// [Optional].
	SeverityNumber int `json:"severityNumber,omitempty"`
	// The severity text (also known as log level). The original string representation as
	// it is known at the source. [Optional].
	SeverityText string `json:"severityText,omitempty"`
	// A value containing the body of the log record. Can be for example a human-readable
	// string message (including multi-line) describing the event in a free form or it can
	// be a structured data composed of arrays and maps of other values. [Optional].
	Body anyValue `json:"body"`
	// Additional attributes that describe the specific event occurrence. [Optional].
	// Attribute keys MUST be unique (it is not allowed to have more than one
	// attribute with the same key).
	Attributes []keyValue `json:"attributes,omitempty"`
}

type Client struct {
	options    *ClientOptions
	httpClient *http.Client

	// To store log entries, keep a buffer of size 2*MaxRequestEntries with a
	// sliding window "entries" of size MaxRequestEntries.
	buffer  []entryWithService
	entries []entryWithService

	// Store the custom labels for each service (resource attributes in OTEL).
	resourceAttributes map[string][]keyValue
}

func NewClient(options *ClientOptions) (*Client, error) {
	opts := *options
	fillDefaultOptions(&opts)

	// Validate URL in FIPS builds
	if err := httputil.ValidateURL(opts.Location); err != nil {
		return nil, fmt.Errorf("invalid location URL for target %q: %w", opts.TargetName, err)
	}

	c := &Client{
		options:            &opts,
		httpClient:         httputil.NewClient(httputil.ClientOptions{Timeout: opts.RequestTimeout}),
		buffer:             make([]entryWithService, 2*opts.MaxRequestEntries),
		resourceAttributes: make(map[string][]keyValue),
	}
	// c.entries should be backed by the same array as c.buffer.
	c.entries = c.buffer[:0]
	return c, nil
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

	// Convert attributes to keyValue format.
	keyValuePairs := make([]keyValue, 0, len(attributes)+1)

	// Add service.name attribute.
	keyValuePairs = append(keyValuePairs, keyValue{
		Key:   "service.name",
		Value: anyValue{StringValue: &serviceName},
	})

	// Sort other labels to ensure deterministic order.
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := attributes[k]
		keyValuePairs = append(keyValuePairs, keyValue{
			Key:   k,
			Value: anyValue{StringValue: &v},
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

func encodeEntry(entry servicelog.Entry) logRecord {
	message := strings.TrimSuffix(entry.Message, "\n")

	return logRecord{
		TimeUnixNano: strconv.FormatInt(entry.Time.UnixNano(), 10),
		Body:         anyValue{StringValue: &message},
	}
}

type payload struct {
	ResourceLogs []resourceLogs `json:"resourceLogs"`
}

// Flush sends the buffered logs to the OpenTelemetry collector.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil // no-op
	}

	// Group entries by service.
	serviceBatches := make(map[string][]logRecord)
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

	logs := make([]resourceLogs, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		batch := serviceBatches[serviceName]

		resourceAttributes := c.resourceAttributes[serviceName]
		resource := resource{
			Attributes: resourceAttributes,
		}
		scope := instrumentationScope{Name: c.options.ScopeName}
		scopeLogs := []scopeLogs{{
			Scope:      scope,
			LogRecords: batch,
		}}
		logs = append(logs, resourceLogs{
			Resource:  resource,
			ScopeLogs: scopeLogs,
		})
	}

	payload := payload{
		ResourceLogs: logs,
	}

	return c.sendBatch(ctx, payload)
}

func (c *Client) sendBatch(ctx context.Context, payload payload) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot marshal log batch: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.options.Location+"/v1/logs", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("cannot create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
	entry   logRecord
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
