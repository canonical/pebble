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
	var resourceSpans []*tracepb.ResourceSpans
	byResource := make(map[*resource.Resource]*tracepb.ResourceSpans)
	byScope := make(map[scopeKey]*tracepb.ScopeSpans)

	for _, span := range spans {
		res := span.Resource()
		rs, ok := byResource[res]
		if !ok {
			rs = &tracepb.ResourceSpans{Resource: resourceProto(res)}
			if res != nil {
				rs.SchemaUrl = res.SchemaURL()
			}
			byResource[res] = rs
			resourceSpans = append(resourceSpans, rs)
		}

		scope := span.InstrumentationScope()
		key := scopeKey{res, scope}
		ss, ok := byScope[key]
		if !ok {
			ss = &tracepb.ScopeSpans{
				Scope: &commonpb.InstrumentationScope{
					Name:       scope.Name,
					Version:    scope.Version,
					Attributes: keyValues(scope.Attributes.ToSlice()),
				},
				SchemaUrl: scope.SchemaURL,
			}
			byScope[key] = ss
			rs.ScopeSpans = append(rs.ScopeSpans, ss)
		}

		ss.Spans = append(ss.Spans, spanProto(span))
	}
	return &tracepb.TracesData{ResourceSpans: resourceSpans}
}

func resourceProto(res *resource.Resource) *resourcepb.Resource {
	if res == nil {
		return &resourcepb.Resource{}
	}
	return &resourcepb.Resource{Attributes: keyValues(res.Attributes())}
}

func spanProto(span sdktrace.ReadOnlySpan) *tracepb.Span {
	sc := span.SpanContext()
	s := &tracepb.Span{
		TraceId:                traceIDBytes(sc.TraceID()),
		SpanId:                 spanIDBytes(sc.SpanID()),
		TraceState:             sc.TraceState().String(),
		Flags:                  spanFlags(sc.TraceFlags(), span.Parent().IsRemote()),
		Name:                   span.Name(),
		Kind:                   tracepb.Span_SpanKind(span.SpanKind()),
		StartTimeUnixNano:      unixNano(span.StartTime()),
		EndTimeUnixNano:        unixNano(span.EndTime()),
		Attributes:             keyValues(span.Attributes()),
		DroppedAttributesCount: uint32(span.DroppedAttributes()),
		DroppedEventsCount:     uint32(span.DroppedEvents()),
		DroppedLinksCount:      uint32(span.DroppedLinks()),
		Status:                 statusProto(span.Status()),
	}
	if parent := span.Parent(); parent.SpanID().IsValid() {
		s.ParentSpanId = spanIDBytes(parent.SpanID())
	}
	for _, event := range span.Events() {
		s.Events = append(s.Events, &tracepb.Span_Event{
			TimeUnixNano:           unixNano(event.Time),
			Name:                   event.Name,
			Attributes:             keyValues(event.Attributes),
			DroppedAttributesCount: uint32(event.DroppedAttributeCount),
		})
	}
	for _, link := range span.Links() {
		s.Links = append(s.Links, &tracepb.Span_Link{
			TraceId:                traceIDBytes(link.SpanContext.TraceID()),
			SpanId:                 spanIDBytes(link.SpanContext.SpanID()),
			TraceState:             link.SpanContext.TraceState().String(),
			Flags:                  spanFlags(link.SpanContext.TraceFlags(), link.SpanContext.IsRemote()),
			Attributes:             keyValues(link.Attributes),
			DroppedAttributesCount: uint32(link.DroppedAttributeCount),
		})
	}
	return s
}

func traceIDBytes(id trace.TraceID) []byte {
	return id[:]
}

func spanIDBytes(id trace.SpanID) []byte {
	return id[:]
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

func statusProto(status sdktrace.Status) *tracepb.Status {
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
	return &tracepb.Status{Code: code, Message: status.Description}
}

func keyValues(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	kvs := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		kvs = append(kvs, &commonpb.KeyValue{
			Key:   string(attr.Key),
			Value: anyValue(attr.Value),
		})
	}
	return kvs
}

func anyValue(v attribute.Value) *commonpb.AnyValue {
	switch v.Type() {
	case attribute.BOOL:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v.AsBool()}}
	case attribute.INT64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v.AsInt64()}}
	case attribute.FLOAT64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v.AsFloat64()}}
	case attribute.STRING:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v.AsString()}}
	case attribute.BYTESLICE:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: v.AsByteSlice()}}
	case attribute.BOOLSLICE:
		return arrayValue(v.AsBoolSlice(), attribute.BoolValue)
	case attribute.INT64SLICE:
		return arrayValue(v.AsInt64Slice(), attribute.Int64Value)
	case attribute.FLOAT64SLICE:
		return arrayValue(v.AsFloat64Slice(), attribute.Float64Value)
	case attribute.STRINGSLICE:
		return arrayValue(v.AsStringSlice(), attribute.StringValue)
	case attribute.SLICE:
		return arrayValue(v.AsSlice(), func(v attribute.Value) attribute.Value { return v })
	case attribute.MAP:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{
			KvlistValue: &commonpb.KeyValueList{Values: keyValues(v.AsMap())},
		}}
	default:
		// EMPTY, or a type added to the API after this was written.
		return &commonpb.AnyValue{}
	}
}

func arrayValue[T any](values []T, toValue func(T) attribute.Value) *commonpb.AnyValue {
	array := &commonpb.ArrayValue{Values: make([]*commonpb.AnyValue, 0, len(values))}
	for _, v := range values {
		array.Values = append(array.Values, anyValue(toValue(v)))
	}
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: array}}
}
