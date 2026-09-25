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

// Package tracing configures OpenTelemetry tracing for Pebble.
//
// Tracing is disabled unless an OTLP endpoint is configured using the
// standard OpenTelemetry environment variables (OTEL_EXPORTER_OTLP_ENDPOINT
// or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT). Traces are exported using OTLP over
// HTTP with the protobuf encoding, and the exporter honours the standard
// OTEL_EXPORTER_OTLP_{,TRACES_}{HEADERS,TIMEOUT,COMPRESSION,CERTIFICATE,
// CLIENT_CERTIFICATE,CLIENT_KEY} variables. Resource attributes may be set
// with OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/canonical/pebble/cmd"
)

// ScopeName is the instrumentation scope name used by Pebble's tracers.
const ScopeName = "github.com/canonical/pebble"

// Tracer returns Pebble's tracer from the global tracer provider. If tracing
// has not been set up, the returned tracer is a no-op.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}

// Enabled reports whether the environment requests traces to be exported.
func Enabled() bool {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return false
	}
	if exporter := os.Getenv("OTEL_TRACES_EXPORTER"); exporter != "" && exporter != "otlp" {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Setup configures the global OpenTelemetry tracer provider and propagator
// from the environment. It returns a function that flushes and shuts down the
// tracer provider, which must be called before the process exits. If tracing
// is not enabled (see Enabled), Setup does nothing and returns a no-op
// shutdown function.
func Setup(ctx context.Context, version string) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }
	if !Enabled() {
		return noop, nil
	}

	config, err := exporterConfigFromEnv()
	if err != nil {
		return noop, fmt.Errorf("cannot create trace exporter: %w", err)
	}
	exporter := newExporter(config, version)

	res, err := resource.Merge(
		resource.NewSchemaless(
			semconv.ServiceName("pebble"),
			semconv.ServiceVersion(version),
		),
		// The environment (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES)
		// takes precedence over the defaults above.
		resource.Environment(),
	)
	if err != nil {
		return noop, fmt.Errorf("cannot create trace resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Shutdown, nil
}

// SpanContextFromEnv returns the span context described by the TRACEPARENT
// and TRACESTATE environment variables, which use the W3C Trace Context
// header formats. The returned span context is invalid if TRACEPARENT is
// unset or malformed.
func SpanContextFromEnv() trace.SpanContext {
	carrier := propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	return trace.SpanContextFromContext(ctx)
}

// AttrKey returns the attribute key for a Pebble-specific attribute, prefixed
// with the program name (for example, AttrKey("change.id") returns
// "pebble.change.id"). It's computed on use rather than once, because
// cmd.ProgramName may be changed at startup.
func AttrKey(name string) attribute.Key {
	return attribute.Key(cmd.ProgramName + "." + name)
}

// Environment variables used to propagate the trace context to child
// processes, as described in the OpenTelemetry specification.
const (
	envTraceParent = "TRACEPARENT"
	envTraceState  = "TRACESTATE"
)

// DeleteEnv removes the trace context variables from env. This is used when
// a child process inherits the daemon's environment, as the daemon's own
// TRACEPARENT doesn't describe the child's parent span.
func DeleteEnv(env map[string]string) {
	delete(env, envTraceParent)
	delete(env, envTraceState)
}

// InjectEnv sets the TRACEPARENT and TRACESTATE variables in env to describe
// the span carried by ctx, so that a child process can continue the trace.
// Nothing is changed if ctx carries no valid span, or if env already has a
// TRACEPARENT, which is taken to be explicitly configured.
func InjectEnv(ctx context.Context, env map[string]string) {
	if _, ok := env[envTraceParent]; ok {
		return
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	if tp := carrier.Get("traceparent"); tp != "" {
		env[envTraceParent] = tp
		if ts := carrier.Get("tracestate"); ts != "" {
			env[envTraceState] = ts
		}
	}
}

// EndSpan records err (if non-nil) on span, sets the span status to error, and
// ends the span.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
