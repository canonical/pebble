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

package tracing

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
)

// tracesData converts spans to their OTLP representation, grouped by
// resource and then by instrumentation scope.
func tracesData(spans []sdktrace.ReadOnlySpan) *tracepb.TracesData {
	type scopeKey struct {
		res   *resource.Resource
		scope instrumentation.Scope
	}
	var c converter
	var resourceSpans []*tracepb.ResourceSpans
	byResource := make(map[*resource.Resource]*tracepb.ResourceSpans)
	byScope := make(map[scopeKey]*tracepb.ScopeSpans)
	// Spans in a batch usually come from the same tracer, so remember the
	// last group used and skip the map lookups when it matches.
	var lastKey scopeKey
	var lastScope *tracepb.ScopeSpans

	bufs := make([]spanBuf, len(spans))
	for i, span := range spans {
		res := span.Resource()
		key := scopeKey{res, span.InstrumentationScope()}
		ss := lastScope
		if ss == nil || key != lastKey {
			rs, ok := byResource[res]
			if !ok {
				rs = &tracepb.ResourceSpans{Resource: c.resource(res)}
				if res != nil {
					rs.SchemaUrl = res.SchemaURL()
				}
				byResource[res] = rs
				resourceSpans = append(resourceSpans, rs)
			}
			ss, ok = byScope[key]
			if !ok {
				scope := key.scope
				ss = &tracepb.ScopeSpans{
					Scope: &commonpb.InstrumentationScope{
						Name:       scope.Name,
						Version:    scope.Version,
						Attributes: c.keyValues(scope.Attributes.ToSlice()),
					},
					SchemaUrl: scope.SchemaURL,
				}
				byScope[key] = ss
				rs.ScopeSpans = append(rs.ScopeSpans, ss)
			}
			lastKey, lastScope = key, ss
		}
		ss.Spans = append(ss.Spans, c.span(&bufs[i], span))
	}
	return &tracepb.TracesData{ResourceSpans: resourceSpans}
}

// converter converts spans to OTLP messages. It is called for every batch
// of spans exported, so rather than allocate each message separately it
// allocates them from arenas shared by the whole batch, and lays out
// messages that always go together (a span and its status, a key and its
// value, and so on) in a single struct.
type converter struct {
	keyValueBufs arena[keyValueBuf]
	keyValuePtrs arena[*commonpb.KeyValue]
	values       arena[commonpb.AnyValue]
	valuePtrs    arena[*commonpb.AnyValue]
	strings      arena[commonpb.AnyValue_StringValue]
	ints         arena[commonpb.AnyValue_IntValue]
	doubles      arena[commonpb.AnyValue_DoubleValue]
	bools        arena[commonpb.AnyValue_BoolValue]
	events       arena[tracepb.Span_Event]
	eventPtrs    arena[*tracepb.Span_Event]
}

func (c *converter) resource(res *resource.Resource) *resourcepb.Resource {
	if res == nil {
		return &resourcepb.Resource{}
	}
	return &resourcepb.Resource{Attributes: c.keyValues(res.Attributes())}
}

// spanBuf is the storage for a span's protobuf message and the fixed-size
// messages and byte arrays it points to.
type spanBuf struct {
	span     tracepb.Span
	status   tracepb.Status
	traceID  trace.TraceID
	spanID   trace.SpanID
	parentID trace.SpanID
}

// span fills buf with the OTLP representation of span and returns it.
func (c *converter) span(buf *spanBuf, span sdktrace.ReadOnlySpan) *tracepb.Span {
	sc := span.SpanContext()
	parent := span.Parent()
	buf.traceID = sc.TraceID()
	buf.spanID = sc.SpanID()
	buf.status = statusProto(span.Status())
	s := &buf.span
	*s = tracepb.Span{
		TraceId:                buf.traceID[:],
		SpanId:                 buf.spanID[:],
		TraceState:             sc.TraceState().String(),
		Flags:                  spanFlags(sc.TraceFlags(), parent.IsRemote()),
		Name:                   span.Name(),
		Kind:                   tracepb.Span_SpanKind(span.SpanKind()),
		StartTimeUnixNano:      unixNano(span.StartTime()),
		EndTimeUnixNano:        unixNano(span.EndTime()),
		Attributes:             c.keyValues(span.Attributes()),
		DroppedAttributesCount: uint32(span.DroppedAttributes()),
		DroppedEventsCount:     uint32(span.DroppedEvents()),
		DroppedLinksCount:      uint32(span.DroppedLinks()),
		Status:                 &buf.status,
	}
	if parent.SpanID().IsValid() {
		buf.parentID = parent.SpanID()
		s.ParentSpanId = buf.parentID[:]
	}

	if events := span.Events(); len(events) > 0 {
		s.Events = c.eventPtrs.alloc(len(events))
		bufs := c.events.alloc(len(events))
		for i, event := range events {
			bufs[i] = tracepb.Span_Event{
				TimeUnixNano:           unixNano(event.Time),
				Name:                   event.Name,
				Attributes:             c.keyValues(event.Attributes),
				DroppedAttributesCount: uint32(event.DroppedAttributeCount),
			}
			s.Events[i] = &bufs[i]
		}
	}

	if links := span.Links(); len(links) > 0 {
		s.Links = make([]*tracepb.Span_Link, len(links))
		bufs := make([]linkBuf, len(links))
		for i, link := range links {
			buf := &bufs[i]
			buf.traceID = link.SpanContext.TraceID()
			buf.spanID = link.SpanContext.SpanID()
			buf.link = tracepb.Span_Link{
				TraceId:                buf.traceID[:],
				SpanId:                 buf.spanID[:],
				TraceState:             link.SpanContext.TraceState().String(),
				Flags:                  spanFlags(link.SpanContext.TraceFlags(), link.SpanContext.IsRemote()),
				Attributes:             c.keyValues(link.Attributes),
				DroppedAttributesCount: uint32(link.DroppedAttributeCount),
			}
			s.Links[i] = &buf.link
		}
	}
	return s
}

// linkBuf is the storage for a link's protobuf message and the byte arrays
// it points to.
type linkBuf struct {
	link    tracepb.Span_Link
	traceID trace.TraceID
	spanID  trace.SpanID
}

// spanFlags returns the OTLP span flags: the W3C trace flags in the low
// byte, plus whether the parent (or linked) span context is remote.
func spanFlags(tf trace.TraceFlags, isRemote bool) uint32 {
	flags := uint32(tf) | uint32(tracepb.SpanFlags_SPAN_FLAGS_CONTEXT_HAS_IS_REMOTE_MASK)
	if isRemote {
		flags |= uint32(tracepb.SpanFlags_SPAN_FLAGS_CONTEXT_IS_REMOTE_MASK)
	}
	return flags
}

func unixNano(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixNano())
}

func statusProto(status sdktrace.Status) tracepb.Status {
	// The OTel API and OTLP number the status codes differently.
	var code tracepb.Status_StatusCode
	switch status.Code {
	case codes.Ok:
		code = tracepb.Status_STATUS_CODE_OK
	case codes.Error:
		code = tracepb.Status_STATUS_CODE_ERROR
	default:
		code = tracepb.Status_STATUS_CODE_UNSET
	}
	return tracepb.Status{Code: code, Message: status.Description}
}

// keyValueBuf is the storage for an attribute's key-value message and the
// value message it points to.
type keyValueBuf struct {
	kv    commonpb.KeyValue
	value commonpb.AnyValue
}

func (c *converter) keyValues(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	kvs := c.keyValuePtrs.alloc(len(attrs))
	bufs := c.keyValueBufs.alloc(len(attrs))
	for i, attr := range attrs {
		buf := &bufs[i]
		buf.kv = commonpb.KeyValue{Key: string(attr.Key), Value: &buf.value}
		c.setAnyValue(&buf.value, attr.Value)
		kvs[i] = &buf.kv
	}
	return kvs
}

// setAnyValue sets dst to the OTLP representation of v.
func (c *converter) setAnyValue(dst *commonpb.AnyValue, v attribute.Value) {
	switch v.Type() {
	case attribute.BOOL:
		dst.Value = c.bools.new(commonpb.AnyValue_BoolValue{BoolValue: v.AsBool()})
	case attribute.INT64:
		dst.Value = c.ints.new(commonpb.AnyValue_IntValue{IntValue: v.AsInt64()})
	case attribute.FLOAT64:
		dst.Value = c.doubles.new(commonpb.AnyValue_DoubleValue{DoubleValue: v.AsFloat64()})
	case attribute.STRING:
		dst.Value = c.strings.new(commonpb.AnyValue_StringValue{StringValue: v.AsString()})
	case attribute.BYTESLICE:
		dst.Value = &commonpb.AnyValue_BytesValue{BytesValue: v.AsByteSlice()}
	case attribute.BOOLSLICE:
		setArrayValue(c, dst, v.AsBoolSlice(), attribute.BoolValue)
	case attribute.INT64SLICE:
		setArrayValue(c, dst, v.AsInt64Slice(), attribute.Int64Value)
	case attribute.FLOAT64SLICE:
		setArrayValue(c, dst, v.AsFloat64Slice(), attribute.Float64Value)
	case attribute.STRINGSLICE:
		setArrayValue(c, dst, v.AsStringSlice(), attribute.StringValue)
	case attribute.SLICE:
		setArrayValue(c, dst, v.AsSlice(), func(v attribute.Value) attribute.Value { return v })
	case attribute.MAP:
		buf := &struct {
			wrapper commonpb.AnyValue_KvlistValue
			list    commonpb.KeyValueList
		}{}
		buf.list.Values = c.keyValues(v.AsMap())
		buf.wrapper.KvlistValue = &buf.list
		dst.Value = &buf.wrapper
	default:
		// EMPTY, or a type added to the API after this was written.
		dst.Value = nil
	}
}

func setArrayValue[T any](c *converter, dst *commonpb.AnyValue, values []T, toValue func(T) attribute.Value) {
	buf := &struct {
		wrapper commonpb.AnyValue_ArrayValue
		array   commonpb.ArrayValue
	}{}
	buf.array.Values = c.valuePtrs.alloc(len(values))
	bufs := c.values.alloc(len(values))
	for i, v := range values {
		c.setAnyValue(&bufs[i], toValue(v))
		buf.array.Values[i] = &bufs[i]
	}
	buf.wrapper.ArrayValue = &buf.array
	dst.Value = &buf.wrapper
}

// arena hands out slices of T from larger chunks, so that many small
// slices and values can be allocated with a few allocations. Pointers into
// an arena stay valid as it grows, as chunks are never reallocated.
type arena[T any] struct {
	chunk []T
}

// arenaMaxChunk limits the chunk size so that the unused tail of the last
// chunk stays small.
const arenaMaxChunk = 256

// alloc returns a zeroed slice of n values with capacity exactly n.
func (a *arena[T]) alloc(n int) []T {
	if n > cap(a.chunk)-len(a.chunk) {
		// The first chunk is only as big as needed, so that converting a
		// single span costs no more than it would without an arena; the
		// chunks then grow as the batch turns out to be bigger.
		size := max(n, min(2*cap(a.chunk), arenaMaxChunk))
		a.chunk = make([]T, 0, size)
	}
	start := len(a.chunk)
	a.chunk = a.chunk[:start+n]
	return a.chunk[start : start+n : start+n]
}

// new returns a pointer to a copy of v.
func (a *arena[T]) new(v T) *T {
	s := a.alloc(1)
	s[0] = v
	return &s[0]
}
