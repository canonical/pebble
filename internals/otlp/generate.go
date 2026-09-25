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

package otlp

//go:generate cotorp -I proto -go_out . -paths source_relative -json_enum_numbers -json_discard_unknown -json_hex opentelemetry.proto.trace.v1.Span.trace_id -json_hex opentelemetry.proto.trace.v1.Span.span_id -json_hex opentelemetry.proto.trace.v1.Span.parent_span_id -json_hex opentelemetry.proto.trace.v1.Span.Link.trace_id -json_hex opentelemetry.proto.trace.v1.Span.Link.span_id common/v1/common.proto resource/v1/resource.proto trace/v1/trace.proto
