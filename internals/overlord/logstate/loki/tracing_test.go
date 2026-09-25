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

package loki_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/logstate/loki"
	"github.com/canonical/pebble/internals/servicelog"
)

func (*suite) TestFlushPropagatesTrace(c *C) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	restore := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(restore)

	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header
		w.WriteHeader(204)
	}))
	defer server.Close()

	// The location may contain credentials, which aren't recorded.
	location := strings.Replace(server.URL, "http://", "http://user:secret@", 1)
	client := loki.NewClient(&loki.ClientOptions{Location: location})
	err := client.Add(servicelog.Entry{
		Time:    time.Now(),
		Service: "svc1",
		Message: "this is a log line\n",
	})
	c.Assert(err, IsNil)

	ctx, flushSpan := tp.Tracer("test").Start(context.Background(), "flush")
	err = client.Flush(ctx)
	flushSpan.End()
	c.Assert(err, IsNil)

	var requestSpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "POST" {
			requestSpan = span
		}
	}
	c.Assert(requestSpan, NotNil)
	c.Check(requestSpan.SpanKind(), Equals, trace.SpanKindClient)
	c.Check(requestSpan.Parent().SpanID(), Equals, flushSpan.SpanContext().SpanID())
	attrs := make(map[string]any)
	for _, kv := range requestSpan.Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	c.Check(attrs["url.full"], Equals, strings.Replace(server.URL, "http://", "http://user:xxxxx@", 1)+"")
	c.Check(attrs["http.response.status_code"], Equals, int64(204))

	// The trace is propagated to the server, from the request's span.
	sc := requestSpan.SpanContext()
	c.Check(headers.Get("traceparent"), Equals, "00-"+sc.TraceID().String()+"-"+sc.SpanID().String()+"-01")
}
