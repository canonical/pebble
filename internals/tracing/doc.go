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

// Package tracing provides OpenTelemetry tracing for Pebble.
//
// Pebble traces the work it does on behalf of its clients and its own
// schedule, so that an operator can see, in their tracing backend, what a
// request caused Pebble to do and how long each part took. Traces are
// exported to any OTLP-compatible collector.
//
// # The only OpenTelemetry import
//
// This package, together with its test helpers in tracingtest, is the only
// part of Pebble that imports OpenTelemetry (go.opentelemetry.io/...)
// directly. The rest of Pebble uses the aliases and wrappers in this package
// (such as [Span], [SpanContext], [Attribute], [WithAttributes] and
// [StatusError]) and the helpers for common kinds of span (such as
// [StartHTTPServerSpan] and [StartHTTPClientSpan]). The aliases are identical
// to the OpenTelemetry types, so values can be passed freely between the two.
//
// Keeping OpenTelemetry behind one package means that its API surface, and
// the semantic conventions version in use, are chosen in one place, and that
// upgrading or replacing it only touches this package. Tests use the
// companion package tracingtest to record spans, rather than setting up the
// OpenTelemetry SDK themselves.
//
// # Enabling tracing
//
// Tracing is disabled unless an OTLP endpoint is configured using the
// standard OpenTelemetry environment variables, in which case [Setup], called
// when the daemon starts, installs a global tracer provider that batches and
// exports spans. When tracing is disabled, [Tracer] returns a no-op tracer,
// so instrumented code costs little and needs no conditionals.
//
// The following variables are honoured, where OTEL_EXPORTER_OTLP_TRACES_*
// takes precedence over the corresponding OTEL_EXPORTER_OTLP_* variable:
//
//   - OTEL_EXPORTER_OTLP_ENDPOINT: the collector's base URL, to which
//     "/v1/traces" is appended; or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, the
//     full URL, used as-is. Either enables tracing.
//   - OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf" (the default) or
//     "http/json". The grpc protocol isn't supported.
//   - OTEL_EXPORTER_OTLP_HEADERS: extra request headers, such as
//     authentication, as comma-separated key=value pairs.
//   - OTEL_EXPORTER_OTLP_TIMEOUT: the export timeout in milliseconds,
//     including retries (default 10000).
//   - OTEL_EXPORTER_OTLP_COMPRESSION: "gzip" or "none" (the default).
//   - OTEL_EXPORTER_OTLP_CERTIFICATE, OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE
//     and OTEL_EXPORTER_OTLP_CLIENT_KEY: PEM files for verifying the
//     collector and for client authentication.
//   - OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES: resource attributes,
//     overriding the defaults of service.name "pebble" and service.version.
//   - OTEL_SDK_DISABLED=true, or OTEL_TRACES_EXPORTER set to anything other
//     than "otlp", disables tracing.
//
// # The OTLP exporter
//
// Spans are exported using OTLP over HTTP, with either the protobuf encoding
// (the default, and the most compact) or the JSON encoding, for collectors
// and proxies that only accept JSON. The exporter is implemented in this
// package rather than using the upstream otlptrace exporters, which depend
// on gRPC even when only HTTP is used, and so would add significantly to
// Pebble's size and dependencies. This exporter needs only the OTLP message
// types generated into internals/otlp, which depend only on the standard
// library.
//
// It relies on the OTLP TracesData message being wire-compatible with
// ExportTraceServiceRequest, so the service definitions aren't needed. The
// JSON encoding follows the OTLP rules, in which trace and span IDs are hex
// rather than base64 and enums are integers; the message types are generated
// to encode JSON this way. Failed exports are retried with backoff when the
// collector responds with 429, 502, 503 or 504, honouring Retry-After.
//
// # Trace context propagation
//
// Trace context is propagated using the W3C Trace Context format
// (traceparent and tracestate), so that Pebble's spans join the traces of
// the systems around it:
//
//   - The pebble CLI sends the trace context from its TRACEPARENT and
//     TRACESTATE environment variables (see [SpanContextFromEnv]) as HTTP
//     headers, and the daemon continues the trace from the headers of each
//     API request (see [StartHTTPServerSpan]).
//   - Outbound HTTP requests can carry the trace context in their headers
//     (see [StartHTTPClientSpan]), and processes run by Pebble can receive
//     it in their TRACEPARENT and TRACESTATE environment variables (see
//     [InjectEnv] and [DeleteEnv]).
//
// # What is traced
//
// Instrumentation lives alongside the code it traces. Each API request is a
// server span (see the daemon package), a child of the trace described by
// the request's headers, if any. The span records who made the request, the
// change it created and the kind and message of an error response. Expected
// 5xx responses, such as a long poll timing out or the health endpoint
// reporting that the system is unhealthy, aren't recorded as errors.
// Authenticating the request is a child span, as it may verify a password or
// certificate while holding the state lock.
//
// Pebble-specific span attributes are prefixed with the program name, such
// as "pebble.change.id" (see [AttrKey]). Standard attributes follow the
// OpenTelemetry semantic conventions.
package tracing
