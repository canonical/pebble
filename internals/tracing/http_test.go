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

package tracing_test

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/tracing"
)

type httpSuite struct {
	recorder *tracetest.SpanRecorder
	tp       *sdktrace.TracerProvider
	restore  trace.TracerProvider
}

var _ = Suite(&httpSuite{})

func (s *httpSuite) SetUpTest(c *C) {
	s.recorder = tracetest.NewSpanRecorder()
	s.tp = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.recorder))
	s.restore = otel.GetTracerProvider()
	otel.SetTracerProvider(s.tp)
}

func (s *httpSuite) TearDownTest(c *C) {
	otel.SetTracerProvider(s.restore)
}

func (s *httpSuite) TestHTTPClientSpan(c *C) {
	ctx, parent := s.tp.Tracer("test").Start(context.Background(), "parent")
	req, err := http.NewRequestWithContext(ctx, "POST", "https://u:p@example.com:8443/push?key=abc", nil)
	c.Assert(err, IsNil)

	req, span := tracing.StartHTTPClientSpan(req)
	sc := span.SpanContext()
	c.Check(trace.SpanContextFromContext(req.Context()).SpanID(), Equals, sc.SpanID())
	c.Check(req.Header.Get("traceparent"), Equals, "00-"+sc.TraceID().String()+"-"+sc.SpanID().String()+"-01")
	tracing.EndHTTPClientSpan(span, &http.Response{StatusCode: 200, Status: "200 OK"}, nil)
	parent.End()

	ended := s.recorder.Ended()
	c.Assert(ended, HasLen, 2)
	got := ended[0]
	c.Check(got.Name(), Equals, "POST")
	c.Check(got.SpanKind(), Equals, trace.SpanKindClient)
	c.Check(got.Parent().SpanID(), Equals, parent.SpanContext().SpanID())
	c.Check(got.Status().Code, Equals, codes.Unset)
	attrs := make(map[string]any)
	for _, kv := range got.Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	c.Check(attrs, DeepEquals, map[string]any{
		"http.request.method":       "POST",
		"url.full":                  "https://u:xxxxx@example.com:8443/push?key=REDACTED",
		"server.address":            "example.com",
		"server.port":               int64(8443),
		"http.response.status_code": int64(200),
	})
}

func (s *httpSuite) TestHTTPClientSpanExistingTraceparent(c *C) {
	ctx, parent := s.tp.Tracer("test").Start(context.Background(), "parent")
	defer parent.End()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://example.com/", nil)
	c.Assert(err, IsNil)
	req.Header.Set("traceparent", "configured")

	req, span := tracing.StartHTTPClientSpan(req)
	span.End()
	c.Check(req.Header.Get("traceparent"), Equals, "configured")
}

func (s *httpSuite) TestHTTPClientSpanErrors(c *C) {
	req, err := http.NewRequest("GET", "http://example.com/", nil)
	c.Assert(err, IsNil)

	// A 4xx or 5xx response is an error.
	_, span := tracing.StartHTTPClientSpan(req)
	tracing.EndHTTPClientSpan(span, &http.Response{StatusCode: 404, Status: "404 Not Found"}, nil)
	// As is a failed request.
	_, span = tracing.StartHTTPClientSpan(req)
	tracing.EndHTTPClientSpan(span, nil, errors.New("connection refused"))

	ended := s.recorder.Ended()
	c.Assert(ended, HasLen, 2)
	c.Check(ended[0].Status(), Equals, sdktrace.Status{Code: codes.Error, Description: "404 Not Found"})
	c.Check(ended[1].Status(), Equals, sdktrace.Status{Code: codes.Error, Description: "connection refused"})
	c.Check(ended[1].Parent().IsValid(), Equals, false)
}

func (s *httpSuite) TestRedactURL(c *C) {
	c.Check(tracing.RedactURL("http://localhost:8080/health"), Equals, "http://localhost:8080/health")
	c.Check(tracing.RedactURL("https://u:p@host/x?a=1&b=2"), Equals, "https://u:xxxxx@host/x?a=REDACTED&b=REDACTED")
	c.Check(tracing.RedactURL("::"), Equals, "")
}
