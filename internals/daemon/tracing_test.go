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

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	. "gopkg.in/check.v1"
)

type tracingSuite struct {
	recorder *tracetest.SpanRecorder
	restore  trace.TracerProvider

	// handlerSpan is the span context seen by the routed handler.
	handlerSpan trace.SpanContext
	router      *http.ServeMux
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.recorder = tracetest.NewSpanRecorder()
	s.restore = otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.recorder)))

	s.handlerSpan = trace.SpanContext{}
	s.router = http.NewServeMux()
	s.router.HandleFunc("/v1/changes/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.handlerSpan = trace.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusAccepted)
	})
	s.router.HandleFunc("/v1/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	s.router.Handle("/", NotFound("invalid API endpoint requested"))
}

func (s *tracingSuite) TearDownTest(c *C) {
	otel.SetTracerProvider(s.restore)
}

func (s *tracingSuite) serve(c *C, req *http.Request) sdktrace.ReadOnlySpan {
	traceRequest(s.router).ServeHTTP(httptest.NewRecorder(), req)
	spans := s.recorder.Ended()
	c.Assert(spans, HasLen, 1)
	return spans[0]
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
}

func (s *tracingSuite) TestServerSpan(c *C) {
	req := httptest.NewRequest("GET", "/v1/changes/42", nil)
	req = req.WithContext(context.WithValue(req.Context(), TransportTypeKey{}, TransportTypeUnixSocket))
	req.Header.Set("User-Agent", "pebble/1.0")
	span := s.serve(c, req)

	c.Check(span.Name(), Equals, "GET /v1/changes/{id}")
	c.Check(span.SpanKind(), Equals, trace.SpanKindServer)
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Check(span.Status().Code, Equals, codes.Unset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.request.method"].AsString(), Equals, "GET")
	c.Check(attrs["http.route"].AsString(), Equals, "/v1/changes/{id}")
	c.Check(attrs["url.path"].AsString(), Equals, "/v1/changes/42")
	c.Check(attrs["url.scheme"].AsString(), Equals, "http")
	c.Check(attrs["network.transport"].AsString(), Equals, "unix")
	c.Check(attrs["user_agent.original"].AsString(), Equals, "pebble/1.0")
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(http.StatusAccepted))

	// The handler sees the server span in the request context.
	c.Check(s.handlerSpan.SpanID(), Equals, span.SpanContext().SpanID())
}

func (s *tracingSuite) TestServerSpanParentFromHeaders(c *C) {
	req := httptest.NewRequest("POST", "/v1/changes/42", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	req.Header.Set("tracestate", "foo=bar")
	span := s.serve(c, req)

	c.Check(span.SpanContext().TraceID().String(), Equals, "0af7651916cd43dd8448eb211c80319c")
	c.Check(span.Parent().SpanID().String(), Equals, "b7ad6b7169203331")
	c.Check(span.Parent().IsRemote(), Equals, true)
	c.Check(span.SpanContext().TraceState().String(), Equals, "foo=bar")
	c.Check(s.handlerSpan.TraceID(), Equals, span.SpanContext().TraceID())
}

func (s *tracingSuite) TestServerSpanTCP(c *C) {
	req := httptest.NewRequest("GET", "/v1/changes/42", nil)
	req = req.WithContext(context.WithValue(req.Context(), TransportTypeKey{}, TransportTypeHTTPS))
	req.RemoteAddr = "10.0.0.1:5555"
	span := s.serve(c, req)

	attrs := spanAttrs(span)
	c.Check(attrs["network.transport"].AsString(), Equals, "tcp")
	c.Check(attrs["url.scheme"].AsString(), Equals, "https")
	c.Check(attrs["client.address"].AsString(), Equals, "10.0.0.1")
}

func (s *tracingSuite) TestServerSpanServerError(c *C) {
	span := s.serve(c, httptest.NewRequest("GET", "/v1/broken", nil))
	c.Check(span.Name(), Equals, "GET /v1/broken")
	c.Check(span.Status().Code, Equals, codes.Error)
	c.Check(spanAttrs(span)["http.response.status_code"].AsInt64(), Equals, int64(500))
}

func (s *tracingSuite) TestServerSpanUnknownEndpoint(c *C) {
	span := s.serve(c, httptest.NewRequest("GET", "/v1/nope", nil))
	c.Check(span.Name(), Equals, "GET")
	_, hasRoute := spanAttrs(span)["http.route"]
	c.Check(hasRoute, Equals, false)
	c.Check(spanAttrs(span)["http.response.status_code"].AsInt64(), Equals, int64(404))
	// 4xx responses aren't server errors.
	c.Check(span.Status().Code, Equals, codes.Unset)
}

func (s *tracingSuite) TestServerSpanUnknownMethod(c *C) {
	span := s.serve(c, httptest.NewRequest("FROB", "/v1/changes/42", nil))
	c.Check(span.Name(), Equals, "HTTP /v1/changes/{id}")
	attrs := spanAttrs(span)
	c.Check(attrs["http.request.method"].AsString(), Equals, "_OTHER")
	c.Check(attrs["http.request.method_original"].AsString(), Equals, "FROB")
}
