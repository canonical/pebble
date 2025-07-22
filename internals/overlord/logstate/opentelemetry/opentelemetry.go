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
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	requestTimeout    = 10 * time.Second
	maxRequestEntries = 100
	batchSize         = 10
)

type Client struct {
	options    *ClientOptions
	target     *plan.LogTarget
	httpClient *http.Client

	// To store log entries, keep a buffer of size 2*MaxRequestEntries with a
	// sliding window "entries" of size MaxRequestEntries.
	buffer  []otelEntryWithService
	entries []otelEntryWithService

	// Store the custom labels for each service (resource attributes in OTEL).
	resourceAttributes map[string][]KeyValue
}

func NewClient(target *plan.LogTarget) *Client {
	return NewClientWithOptions(target, &ClientOptions{})
}

// ClientOptions allows overriding default parameters (e.g. for testing).
type ClientOptions struct {
	RequestTimeout    time.Duration
	MaxRequestEntries int
	BatchSize         int
}

func NewClientWithOptions(target *plan.LogTarget, options *ClientOptions) *Client {
	options = fillDefaultOptions(options)
	c := &Client{
		options:            options,
		target:             target,
		httpClient:         &http.Client{Timeout: options.RequestTimeout},
		buffer:             make([]otelEntryWithService, 2*options.MaxRequestEntries),
		resourceAttributes: make(map[string][]KeyValue),
	}
	// c.entries should be backed by the same array as c.buffer.
	c.entries = c.buffer[:0]
	return c
}

func fillDefaultOptions(options *ClientOptions) *ClientOptions {
	if options.RequestTimeout == 0 {
		options.RequestTimeout = requestTimeout
	}
	if options.MaxRequestEntries == 0 {
		options.MaxRequestEntries = maxRequestEntries
	}
	if options.BatchSize == 0 {
		options.BatchSize = batchSize
	}
	return options
}

// SetLabels sets resource attributes for a service. Labels are analogous to OpenTelemetry's resource attributes.
func (c *Client) SetLabels(serviceName string, attributes map[string]string) {
	if attributes == nil {
		delete(c.resourceAttributes, serviceName)
		return
	}

	// Convert attributes to KeyValue format.
	keyValuePairs := make([]KeyValue, 0, len(attributes)+1)

	// Add service.name attribute.
	keyValuePairs = append(keyValuePairs, KeyValue{
		Key:   "service.name",
		Value: AttributeValue{StringValue: &serviceName},
	})

	for k, v := range attributes {
		keyValuePairs = append(keyValuePairs, KeyValue{
			Key:   k,
			Value: AttributeValue{StringValue: &v},
		})
	}

	c.resourceAttributes[serviceName] = keyValuePairs
}

func (c *Client) Add(entry servicelog.Entry) error {
	if n := len(c.entries); n >= c.options.MaxRequestEntries {
		// "entries" is full - remove the first element to make room.
		// Zero the removed element to allow garbage collection.
		c.entries[0] = otelEntryWithService{}
		c.entries = c.entries[1:]
	}

	if len(c.entries) >= cap(c.entries) {
		// Copy all the elements to the start of the buffer.
		copy(c.buffer, c.entries)

		// Reset the view into the buffer.
		c.entries = c.buffer[:len(c.entries):len(c.buffer)]

		// Zero removed elements to allow garbage collection.
		for i := len(c.entries); i < len(c.buffer); i++ {
			c.buffer[i] = otelEntryWithService{}
		}
	}

	c.entries = append(c.entries, otelEntryWithService{
		entry:   encodeEntry(entry),
		service: entry.Service,
	})
	return nil
}

func encodeEntry(entry servicelog.Entry) LogRecord {
	message := strings.TrimSuffix(entry.Message, "\n")

	return LogRecord{
		TimeUnixNano: uint64(entry.Time.UnixNano()),
		Body:         AttributeValue{StringValue: &message},
		Attributes:   []KeyValue{},
	}
}

// Flush sends the buffered logs to the OpenTelemetry collector.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil // no-op
	}

	// Group entries by service.
	serviceBatches := make(map[string][]*LogRecord)
	for _, otelEntryWithService := range c.entries {
		serviceName := otelEntryWithService.service
		logRecord := &otelEntryWithService.entry
		serviceBatches[serviceName] = append(serviceBatches[serviceName], logRecord)
	}

	serviceNames := make([]string, 0, len(serviceBatches))
	for serviceName := range serviceBatches {
		serviceNames = append(serviceNames, serviceName)
	}
	// Sort service names to ensure deterministic order.
	sort.Strings(serviceNames)

	resourceLogs := make([]ResourceLogs, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		batch := serviceBatches[serviceName]
		if len(batch) > 0 {
			resourceAttributes := c.resourceAttributes[serviceName]
			resource := Resource{
				Attributes: resourceAttributes,
			}
			scope := Scope{
				Name:    "pebble",
				Version: cmd.Version,
			}
			scopeLogs := []*ScopeLogs{
				{
					Scope:      scope,
					LogRecords: batch,
				},
			}
			resourceLogs = append(resourceLogs, ResourceLogs{
				Resource:  resource,
				ScopeLogs: scopeLogs,
			})
		}
	}

	payload := map[string]any{
		"resourceLogs": resourceLogs,
	}

	if len(resourceLogs) > 0 {
		if err := c.sendBatch(ctx, payload); err != nil {
			return err
		}
	}

	c.resetBuffer() // Reset buffer after successful send.
	return nil
}

// resetBuffer drops all buffered logs (in the case of a successful send, or an unrecoverable error).
func (c *Client) resetBuffer() {
	// Zero removed elements to allow garbage collection.
	for i := 0; i < len(c.entries); i++ {
		c.entries[i] = otelEntryWithService{}
	}
	c.entries = c.buffer[:0]
}

type otelEntryWithService struct {
	entry   LogRecord
	service string
}

func (c *Client) sendBatch(ctx context.Context, payload map[string]any) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling log batch: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.target.Location+"/v1/logs", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("pebble/%s", cmd.Version))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending logs: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var responseBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&responseBody); err == nil {
			log.Printf("Error response: %+v", responseBody)
		} else {
			log.Printf("Failed to decode error response: %v", err)
		}
		return fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	return nil
}

// A collection of ScopeLogs from a Resource.
// Refer to 'type ResourceLogs struct' in
// opentelemetry-collector/pdata/internal/data/protogen/logs/v1/logs.pb.go
type ResourceLogs struct {
	// The resource for the logs in this message.
	// If this field is not set then resource info is unknown.
	Resource Resource `json:"resource"`
	// A list of ScopeLogs that originate from a resource.
	ScopeLogs []*ScopeLogs `json:"scope_logs,omitempty"`
}

// Resource information, partially from 'type Resource struct' in
// opentelemetry-collector/pdata/internal/data/protogen/resource/v1/resource.pb.go
type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

type KeyValue struct {
	Key   string         `json:"key"`
	Value AttributeValue `json:"value"`
}

// AttributeValue represents the OTLP attribute value format.
// Refer to 'type AnyValue struct' in
// opentelemetry-collector/pdata/internal/data/protogen/common/v1/common.pb.go
type AttributeValue struct {
	StringValue *string      `json:"stringValue,omitempty"`
	BoolValue   *bool        `json:"boolValue,omitempty"`
	IntValue    *int64       `json:"intValue,omitempty"`
	DoubleValue *float64     `json:"doubleValue,omitempty"`
	ArrayValue  *ArrayValue  `json:"arrayValue,omitempty"`
	KvlistValue *KvlistValue `json:"kvlistValue,omitempty"`
	BytesValue  []byte       `json:"bytesValue,omitempty"`
}

type ArrayValue struct {
	Values []AttributeValue `json:"values"`
}

type KvlistValue struct {
	Values []KeyValue `json:"values"`
}

// A collection of Logs produced by a Scope.
// Refer to 'type ScopeLogs struct' in
// opentelemetry-collector/pdata/internal/data/protogen/logs/v1/logs.pb.go
type ScopeLogs struct {
	// The instrumentation scope information for the logs in this message.
	// Semantically when InstrumentationScope isn't set, it is equivalent with
	// an empty instrumentation scope name (unknown).
	Scope Scope `json:"scope"`
	// A list of log records.
	LogRecords []*LogRecord `json:"log_records,omitempty"`
}

// Scope is a message representing the instrumentation scope information
// such as the fully qualified name and version.
// Refer to `type InstrumentationScope struct` in
// opentelemetry-collector/pdata/internal/data/protogen/common/v1/common.pb.go
type Scope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// A log record according to OpenTelemetry Log Data Model:
// https://github.com/open-telemetry/oteps/blob/main/text/logs/0097-log-data-model.md
// Refer to `type LogRecord struct` in
// opentelemetry-collector/pdata/internal/data/protogen/logs/v1/logs.pb.go
type LogRecord struct {
	// time_unix_nano is the time when the event occurred.
	// Value is UNIX Epoch time in nanoseconds since 00:00:00 UTC on 1 January 1970.
	// Value of 0 indicates unknown or missing timestamp.
	TimeUnixNano uint64 `json:"timeUnixNano"`
	// The severity text (also known as log level). The original string representation as
	// it is known at the source. [Optional].
	SeverityText string `json:"severityText"`
	// Numerical value of the severity, normalized to values described in Log Data Model.
	// [Optional].
	SeverityNumber int `json:"severityNumber"`
	// A value containing the body of the log record. Can be for example a human-readable
	// string message (including multi-line) describing the event in a free form or it can
	// be a structured data composed of arrays and maps of other values. [Optional].
	Body AttributeValue `json:"body"`
	// Additional attributes that describe the specific event occurrence. [Optional].
	// Attribute keys MUST be unique (it is not allowed to have more than one
	// attribute with the same key).
	Attributes []KeyValue `json:"attributes,omitempty"`
}
