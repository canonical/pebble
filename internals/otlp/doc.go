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

// Package otlp holds the OpenTelemetry Protocol (OTLP) protobuf definitions
// that Pebble uses to export traces, and the Go code generated from them.
//
// This package itself contains no code. The generated message types are in
// its subpackages, one per protobuf package:
//
//   - common/v1: values shared by all signals, such as AnyValue, KeyValue
//     and InstrumentationScope.
//   - resource/v1: Resource, describing the entity producing telemetry.
//   - trace/v1: TracesData and the ResourceSpans, ScopeSpans and Span
//     messages it contains.
//
// # Why the definitions are vendored
//
// The tracing package exports spans with its own OTLP/HTTP exporter, which
// sends protobuf- or JSON-encoded requests. The upstream Go bindings for these
// messages (go.opentelemetry.io/proto/otlp) also contain the OTLP collector
// service definitions, and so depend on gRPC, as do the upstream OTLP
// exporters that use them. Pebble only speaks OTLP over HTTP, so rather than
// take on gRPC, the few message definitions it needs are copied here and
// compiled with protoc-gen-go, which depends only on the protobuf runtime
// (google.golang.org/protobuf).
//
// Only the data model is included, not the collector service
// (opentelemetry/proto/collector/trace/v1), whose request message,
// ExportTraceServiceRequest, is what an OTLP/HTTP endpoint expects. The
// exporter instead sends TracesData, which is wire-compatible with it: both
// have the repeated ResourceSpans as field 1. Likewise, the exporter doesn't
// decode the response message (ExportTraceServiceResponse): it acts on the
// HTTP status code, ignores any partial success, and reports the body of an
// error response as-is.
//
// # Source and modifications
//
// The .proto files in the proto directory are copied from the
// opentelemetry-proto repository (https://github.com/open-telemetry/opentelemetry-proto),
// under the Apache License, Version 2.0, which is retained in each file.
// They are modified only as needed to generate code in this package:
//
//   - Imports are relative to the proto directory (for example,
//     "common/v1/common.proto" rather than
//     "opentelemetry/proto/common/v1/common.proto").
//   - The go_package options name the subpackages of this package.
//
// The protobuf package names (opentelemetry.proto.*) are unchanged, so the
// messages are the standard OTLP messages on the wire and in the protobuf
// registry. Don't import go.opentelemetry.io/proto/otlp in the same binary:
// registering the same fully qualified message names twice is a conflict,
// which the protobuf runtime reports by panicking at startup.
//
// # Regenerating
//
// To update the definitions, copy the new versions of the .proto files from
// opentelemetry-proto, reapply the modifications above, and run:
//
//	go generate ./internals/otlp
//
// This requires protoc and protoc-gen-go on the PATH. Use the version of
// protoc-gen-go that matches the google.golang.org/protobuf version in
// go.mod, as recorded in the header of each generated file.
package otlp
