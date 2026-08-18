// Copyright (c) 2026 Canonical Ltd
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

// Package opentelemetry implements a tracestate client that forwards enriched
// OTLP traces to a remote OpenTelemetry/HTTP collector.
package opentelemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/canonical/pebble/internals/logger"
	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
)

const (
	requestTimeout     = 10 * time.Second
	maxBufferedBatches = 100
)

// ClientOptions allows overriding default parameters (e.g. for testing).
type ClientOptions struct {
	RequestTimeout     time.Duration
	MaxBufferedBatches int
	UserAgent          string
	TargetName         string
	Location           string
}

func fillDefaultOptions(options *ClientOptions) {
	if options.RequestTimeout == 0 {
		options.RequestTimeout = requestTimeout
	}
	if options.MaxBufferedBatches == 0 {
		options.MaxBufferedBatches = maxBufferedBatches
	}
}

// batch holds a single received OTLP export, along with the information
// required to enrich its resource attributes at flush time.
type batch struct {
	service       string
	instanceID    string
	labels        map[string]string
	resourceSpans []*tracepb.ResourceSpans
}

// Client sends enriched OTLP traces to a remote OpenTelemetry/HTTP collector,
// encoded as JSON.
type Client struct {
	options    *ClientOptions
	httpClient *http.Client

	batches []batch
}

// NewClient creates a new Client for the trace target described by options.
func NewClient(options *ClientOptions) *Client {
	opts := *options
	fillDefaultOptions(&opts)
	return &Client{
		options:    &opts,
		httpClient: &http.Client{Timeout: opts.RequestTimeout},
	}
}

// Add adds the given batch of resource spans to the client's buffer, to be
// enriched and sent on the next Flush.
func (c *Client) Add(service, instanceID string, labels map[string]string, resourceSpans []*tracepb.ResourceSpans) error {
	if len(c.batches) >= c.options.MaxBufferedBatches {
		// Buffer is full - drop the oldest batch to make room.
		logger.Noticef("Target %q: trace buffer full, dropping oldest batch", c.options.TargetName)
		c.batches[0] = batch{}
		c.batches = c.batches[1:]
	}

	c.batches = append(c.batches, batch{
		service:       service,
		instanceID:    instanceID,
		labels:        labels,
		resourceSpans: resourceSpans,
	})
	return nil
}

// Flush sends the buffered traces to the OpenTelemetry collector.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.batches) == 0 {
		return nil
	}

	resourceSpans := make([]*tracepb.ResourceSpans, 0, len(c.batches))
	for _, b := range c.batches {
		for _, rs := range b.resourceSpans {
			resourceSpans = append(resourceSpans, enrich(rs, b.service, b.instanceID, b.labels))
		}
	}

	data := &tracepb.TracesData{
		ResourceSpans: resourceSpans,
	}

	return c.sendBatch(ctx, data)
}

// enrich returns a copy of rs with its resource attributes merged with the
// standard Pebble enrichment attributes and the target's custom labels. The
// original ResourceSpans (and its nested ScopeSpans) are not mutated, since
// they may be shared with other targets' clients.
func enrich(rs *tracepb.ResourceSpans, service, instanceID string, labels map[string]string) *tracepb.ResourceSpans {
	var existingAttrs []*commonpb.KeyValue
	var droppedCount uint32
	if res := rs.GetResource(); res != nil {
		existingAttrs = res.GetAttributes()
		droppedCount = res.GetDroppedAttributesCount()
	}

	return &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{
			Attributes:             mergeAttributes(existingAttrs, service, instanceID, labels),
			DroppedAttributesCount: droppedCount,
		},
		ScopeSpans: rs.GetScopeSpans(),
		SchemaUrl:  rs.GetSchemaUrl(),
	}
}

// mergeAttributes merges the standard enrichment attributes and custom target
// labels into an existing set of resource attributes, per the enrichment rules:
//   - service.name is always overridden with service.
//   - pebble.service is always added, set to service.
//   - service.instance.id is added only if not already present.
//   - custom labels always override any conflicting existing attribute.
func mergeAttributes(existing []*commonpb.KeyValue, service, instanceID string, labels map[string]string) []*commonpb.KeyValue {
	result := make([]*commonpb.KeyValue, len(existing))
	copy(result, existing)

	index := make(map[string]int, len(result))
	for i, kv := range result {
		index[kv.GetKey()] = i
	}

	set := func(key, value string) {
		kv := &commonpb.KeyValue{
			Key:   key,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
		}
		if i, ok := index[key]; ok {
			result[i] = kv
			return
		}
		index[key] = len(result)
		result = append(result, kv)
	}
	addIfAbsent := func(key, value string) {
		if _, ok := index[key]; ok {
			return
		}
		set(key, value)
	}

	set("service.name", service)
	set("pebble.service", service)
	if instanceID != "" {
		addIfAbsent("service.instance.id", instanceID)
	}

	// Sort label keys to ensure deterministic output.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		set(k, labels[k])
	}

	return result
}

func (c *Client) sendBatch(ctx context.Context, data *tracepb.TracesData) error {
	jsonData, err := protojson.Marshal(data)
	if err != nil {
		return fmt.Errorf("cannot marshal trace batch: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.options.Location+"/v1/traces", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("cannot create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.options.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot send traces: %v", err)
	}

	return c.handleServerResponse(resp)
}

// resetBuffer drops all buffered batches.
func (c *Client) resetBuffer() {
	for i := range c.batches {
		c.batches[i] = batch{}
	}
	c.batches = c.batches[:0]
}

// handleServerResponse determines what to do based on the response from the
// OpenTelemetry collector. 4xx and 5xx responses indicate errors, so in this
// case, we bubble up the error to the caller.
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
		c.resetBuffer()
		return nil

	case code == http.StatusTooManyRequests:
		return errFromResponse(resp)

	case 400 <= code && code < 500:
		logger.Noticef("Target %q: request failed with status %d, dropping %d batches",
			c.options.TargetName, code, len(c.batches))
		c.resetBuffer()
		return errFromResponse(resp)

	case 500 <= code && code < 600:
		return errFromResponse(resp)

	default:
		return fmt.Errorf("unexpected response from server: %v", resp.Status)
	}
}

// errFromResponse generates an error from a failed *http.Response.
func errFromResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err == nil {
		logger.Debugf("HTTP %d error, response %q", resp.StatusCode, body)
	} else {
		logger.Debugf("HTTP %d error, but cannot read response: %v", resp.StatusCode, err)
	}

	return fmt.Errorf("server returned HTTP %v", resp.Status)
}
