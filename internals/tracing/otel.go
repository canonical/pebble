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
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// This file re-exports the parts of the OpenTelemetry API used by the rest of
// Pebble, so that this package is the only one that imports OpenTelemetry
// directly. The aliases are identical to the OpenTelemetry types.

type (
	Span                 = trace.Span
	SpanContext          = trace.SpanContext
	SpanContextConfig    = trace.SpanContextConfig
	TraceID              = trace.TraceID
	SpanID               = trace.SpanID
	TraceFlags           = trace.TraceFlags
	TraceState           = trace.TraceState
	SpanKind             = trace.SpanKind
	Link                 = trace.Link
	SpanStartOption      = trace.SpanStartOption
	SpanEndOption        = trace.SpanEndOption
	SpanStartEventOption = trace.SpanStartEventOption
	SpanEventOption      = trace.SpanEventOption
	TracerProvider       = trace.TracerProvider

	Attribute      = attribute.KeyValue
	AttributeKey   = attribute.Key
	AttributeValue = attribute.Value

	StatusCode = codes.Code
)

const (
	FlagsSampled = trace.FlagsSampled

	SpanKindInternal = trace.SpanKindInternal
	SpanKindServer   = trace.SpanKindServer
	SpanKindClient   = trace.SpanKindClient

	StatusUnset = codes.Unset
	StatusError = codes.Error
	StatusOK    = codes.Ok
)

// NewSpanContext returns a span context created from config.
func NewSpanContext(config SpanContextConfig) SpanContext {
	return trace.NewSpanContext(config)
}

// SpanFromContext returns the span carried by ctx, or a no-op span.
func SpanFromContext(ctx context.Context) Span {
	return trace.SpanFromContext(ctx)
}

// SpanContextFromContext returns the context of the span carried by ctx.
func SpanContextFromContext(ctx context.Context) SpanContext {
	return trace.SpanContextFromContext(ctx)
}

// ContextWithSpan returns a copy of parent carrying span.
func ContextWithSpan(parent context.Context, span Span) context.Context {
	return trace.ContextWithSpan(parent, span)
}

// ContextWithSpanContext returns a copy of parent carrying sc as its span.
func ContextWithSpanContext(parent context.Context, sc SpanContext) context.Context {
	return trace.ContextWithSpanContext(parent, sc)
}

// ContextWithRemoteSpanContext returns a copy of parent carrying sc, marked
// as coming from another process, as its span.
func ContextWithRemoteSpanContext(parent context.Context, sc SpanContext) context.Context {
	return trace.ContextWithRemoteSpanContext(parent, sc)
}

// WithAttributes sets attributes on a span when it starts, or on an event.
func WithAttributes(attrs ...Attribute) SpanStartEventOption {
	return trace.WithAttributes(attrs...)
}

// WithTimestamp sets the time a span starts or ends, or an event occurs.
func WithTimestamp(t time.Time) SpanEventOption {
	return trace.WithTimestamp(t)
}

// WithLinks links a span to other spans when it starts.
func WithLinks(links ...Link) SpanStartOption {
	return trace.WithLinks(links...)
}

// WithNewRoot starts a span as the root of a new trace, ignoring any parent
// carried by the context.
func WithNewRoot() SpanStartOption {
	return trace.WithNewRoot()
}

// WithSpanKind sets the kind of a span when it starts.
func WithSpanKind(kind SpanKind) SpanStartOption {
	return trace.WithSpanKind(kind)
}

// FormatTraceParent encodes sc as a W3C traceparent value, or returns "" if
// sc is not valid.
func FormatTraceParent(sc SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ContextWithSpanContext(context.Background(), sc), carrier)
	return carrier.Get("traceparent")
}

// ParseTraceParent decodes W3C traceparent and tracestate values, returning an
// invalid span context if they cannot be decoded.
func ParseTraceParent(traceParent, traceState string) SpanContext {
	if traceParent == "" {
		return SpanContext{}
	}
	carrier := propagation.MapCarrier{
		"traceparent": traceParent,
		"tracestate":  traceState,
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	return SpanContextFromContext(ctx)
}

// Semantic convention attributes used outside this package.

// NetworkTransportTCP is the network.transport attribute for TCP.
var NetworkTransportTCP = semconv.NetworkTransportTCP

// ServerAddress returns the server.address attribute.
func ServerAddress(host string) Attribute {
	return semconv.ServerAddress(host)
}

// ServerPort returns the server.port attribute.
func ServerPort(port int) Attribute {
	return semconv.ServerPort(port)
}

// ProcessExecutableName returns the process.executable.name attribute.
func ProcessExecutableName(name string) Attribute {
	return semconv.ProcessExecutableName(name)
}

// ProcessPID returns the process.pid attribute.
func ProcessPID(pid int) Attribute {
	return semconv.ProcessPID(pid)
}

// ProcessExitCode returns the process.exit.code attribute.
func ProcessExitCode(code int) Attribute {
	return semconv.ProcessExitCode(code)
}
