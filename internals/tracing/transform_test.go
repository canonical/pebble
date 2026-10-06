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
	"errors"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// benchSpans records spans using a real tracer provider, the same way the
// batch span processor hands them to the exporter, and returns the ended
// span snapshots.
func benchSpans(n int, record func(tp *sdktrace.TracerProvider, i int)) []sdktrace.ReadOnlySpan {
	recorder := &spanRecorder{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(recorder),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", "pebble"),
			attribute.String("service.version", "1.2.3"),
			attribute.String("service.instance.id", "0123456789abcdef"),
			attribute.String("host.name", "host"),
			attribute.Int("process.pid", 12345),
		)),
	)
	defer tp.Shutdown(context.Background())
	for i := 0; i < n; i++ {
		record(tp, i)
	}
	return recorder.spans
}

// benchSpanMinimal is a span with no attributes, events or links.
func benchSpanMinimal(tp *sdktrace.TracerProvider, i int) {
	_, span := tp.Tracer("pebble").Start(context.Background(), "minimal")
	span.End()
}

// benchSpanTypical is roughly what an HTTP server span looks like: a remote
// parent, a handful of attributes of mixed types and an OK status.
func benchSpanTypical(tp *sdktrace.TracerProvider, i int) {
	remoteParent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{byte(i), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, byte(i)},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), remoteParent)
	ctx, span := tp.Tracer("pebble/http").Start(ctx, "GET /v1/services",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.request.method", "GET"),
			attribute.String("url.path", "/v1/services"),
			attribute.String("http.route", "/v1/services"),
			attribute.String("user_agent.original", "pebble/1.2.3"),
			attribute.Int("http.response.status_code", 200),
			attribute.Int64("http.response.body.size", 4096),
			attribute.Bool("pebble.admin", true),
			attribute.Float64("pebble.elapsed", 1.25),
		))
	_, child := tp.Tracer("pebble/overlord").Start(ctx, "change",
		trace.WithAttributes(attribute.String("change.id", fmt.Sprint(i))))
	child.SetStatus(codes.Ok, "")
	child.End()
	span.SetStatus(codes.Ok, "")
	span.End()
}

// benchSpanHeavy has many attributes (including slices and nested maps),
// several events, a link and an error status.
func benchSpanHeavy(tp *sdktrace.TracerProvider, i int) {
	linked := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, byte(i)},
		SpanID:     trace.SpanID{9, 9, 9, 9, 9, 9, 9, byte(i)},
		TraceFlags: trace.FlagsSampled,
	})
	attrs := make([]attribute.KeyValue, 0, 24)
	for j := 0; j < 8; j++ {
		attrs = append(attrs,
			attribute.String(fmt.Sprintf("str.%d", j), "some string value"),
			attribute.Int(fmt.Sprintf("int.%d", j), j),
		)
	}
	attrs = append(attrs,
		attribute.StringSlice("strs", []string{"a", "b", "c", "d"}),
		attribute.IntSlice("ints", []int{1, 2, 3, 4}),
		attribute.Float64Slice("floats", []float64{1, 2, 3, 4}),
		attribute.BoolSlice("bools", []bool{true, false, true, false}),
		attribute.KeyValue{Key: "nested", Value: attribute.MapValue(
			attribute.String("k1", "v1"),
			attribute.Int("k2", 2),
			attribute.KeyValue{Key: "k3", Value: attribute.SliceValue(
				attribute.StringValue("x"), attribute.IntValue(1), attribute.BoolValue(true),
			)},
		)},
	)
	_, span := tp.Tracer("pebble/heavy").Start(context.Background(), "heavy",
		trace.WithAttributes(attrs...),
		trace.WithLinks(trace.Link{SpanContext: linked, Attributes: []attribute.KeyValue{
			attribute.String("link.reason", "follows-from"),
		}}),
	)
	for j := 0; j < 5; j++ {
		span.AddEvent("retry", trace.WithAttributes(
			attribute.Int("attempt", j),
			attribute.String("reason", "busy"),
		))
	}
	span.RecordError(errors.New("boom"))
	span.SetStatus(codes.Error, "boom")
	span.End()
}

func BenchmarkTracesData(b *testing.B) {
	benchmarks := []struct {
		name   string
		n      int
		record func(tp *sdktrace.TracerProvider, i int)
	}{
		{"minimal", 1, benchSpanMinimal},
		{"typical", 1, benchSpanTypical},
		{"heavy", 1, benchSpanHeavy},
		// A full batch as the batch span processor would export it.
		{"batch", 256, func(tp *sdktrace.TracerProvider, i int) {
			switch i % 8 {
			case 0:
				benchSpanHeavy(tp, i)
			case 1, 2:
				benchSpanMinimal(tp, i)
			default:
				benchSpanTypical(tp, i)
			}
		}},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			spans := benchSpans(bm.n, bm.record)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data := tracesData(spans)
				if len(data.ResourceSpans) == 0 {
					b.Fatal("no resource spans")
				}
			}
		})
	}
}

func BenchmarkKeyValues(b *testing.B) {
	benchmarks := []struct {
		name  string
		attrs []attribute.KeyValue
	}{
		{"scalars", []attribute.KeyValue{
			attribute.String("s", "value"),
			attribute.Int("i", 42),
			attribute.Bool("b", true),
			attribute.Float64("f", 1.5),
		}},
		{"strings", func() []attribute.KeyValue {
			attrs := make([]attribute.KeyValue, 16)
			for i := range attrs {
				attrs[i] = attribute.String(fmt.Sprintf("key.%d", i), "value")
			}
			return attrs
		}()},
		{"slices", []attribute.KeyValue{
			attribute.StringSlice("ss", []string{"a", "b", "c", "d"}),
			attribute.IntSlice("is", []int{1, 2, 3, 4}),
		}},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var c converter
				if kvs := c.keyValues(bm.attrs); len(kvs) != len(bm.attrs) {
					b.Fatal("wrong length")
				}
			}
		})
	}
}
