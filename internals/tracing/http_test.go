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

func (s *httpSuite) TestHTTPServerSpan(c *C) {
	req, err := http.NewRequest("GET", "http://localhost/v1/changes/42", nil)
	c.Assert(err, IsNil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	req.Header.Set("User-Agent", "pebble/1.0")
	req.RemoteAddr = "10.0.0.1:5555"

	req, span := tracing.StartHTTPServerSpan(req, tracing.ServerTransportHTTPS)
	c.Check(trace.SpanContextFromContext(req.Context()).SpanID(), Equals, span.SpanContext().SpanID())
	// The router would set the matched pattern on the request.
	req.Pattern = "/v1/changes/{id}"
	tracing.EndHTTPServerSpan(span, req, tracing.HTTPServerResult{Status: 200})

	ended := s.recorder.Ended()
	c.Assert(ended, HasLen, 1)
	got := ended[0]
	c.Check(got.Name(), Equals, "GET /v1/changes/{id}")
	c.Check(got.SpanKind(), Equals, trace.SpanKindServer)
	c.Check(got.SpanContext().TraceID().String(), Equals, "0af7651916cd43dd8448eb211c80319c")
	c.Check(got.Parent().SpanID().String(), Equals, "b7ad6b7169203331")
	c.Check(got.Parent().IsRemote(), Equals, true)
	c.Check(got.Status().Code, Equals, codes.Unset)
	attrs := make(map[string]any)
	for _, kv := range got.Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	c.Check(attrs, DeepEquals, map[string]any{
		"http.request.method":       "GET",
		"http.route":                "/v1/changes/{id}",
		"url.path":                  "/v1/changes/42",
		"url.scheme":                "https",
		"network.transport":         "tcp",
		"client.address":            "10.0.0.1",
		"user_agent.original":       "pebble/1.0",
		"http.response.status_code": int64(200),
	})
}

func (s *httpSuite) TestHTTPServerSpanStatus(c *C) {
	tests := []struct {
		result tracing.HTTPServerResult
		status sdktrace.Status
	}{{
		// 2xx and 4xx responses aren't server errors.
		result: tracing.HTTPServerResult{Status: 202},
		status: sdktrace.Status{Code: codes.Unset},
	}, {
		result: tracing.HTTPServerResult{Status: 404, ErrorMessage: "cannot find change"},
		status: sdktrace.Status{Code: codes.Unset},
	}, {
		// A 5xx response is an error, described by the status text...
		result: tracing.HTTPServerResult{Status: 500},
		status: sdktrace.Status{Code: codes.Error, Description: "Internal Server Error"},
	}, {
		// ...unless the error response's message is known.
		result: tracing.HTTPServerResult{Status: 500, ErrorMessage: "cannot do the thing"},
		status: sdktrace.Status{Code: codes.Error, Description: "cannot do the thing"},
	}, {
		result: tracing.HTTPServerResult{Status: 504, ErrorMessage: "timed out waiting for change after 1s"},
		status: sdktrace.Status{Code: codes.Error, Description: "timed out waiting for change after 1s"},
	}, {
		// An expected 5xx response (such as a long poll timing out) isn't
		// a failure of the server.
		result: tracing.HTTPServerResult{Status: 504, ErrorMessage: "timed out", Expected: true},
		status: sdktrace.Status{Code: codes.Unset},
	}, {
		result: tracing.HTTPServerResult{Status: 502, Expected: true},
		status: sdktrace.Status{Code: codes.Unset},
	}}
	for _, test := range tests {
		s.recorder.Reset()
		req, err := http.NewRequest("GET", "http://localhost/v1/thing", nil)
		c.Assert(err, IsNil)
		req, span := tracing.StartHTTPServerSpan(req, tracing.ServerTransportUnixSocket)
		tracing.EndHTTPServerSpan(span, req, test.result)

		ended := s.recorder.Ended()
		c.Assert(ended, HasLen, 1)
		c.Check(ended[0].Status(), Equals, test.status, Commentf("%+v", test.result))
		attrs := make(map[string]any)
		for _, kv := range ended[0].Attributes() {
			attrs[string(kv.Key)] = kv.Value.AsInterface()
		}
		c.Check(attrs["http.response.status_code"], Equals, int64(test.result.Status))
	}
}

func (s *httpSuite) TestHTTPServerSpanRoute(c *C) {
	// The catch-all pattern isn't a meaningful route, so the span is named
	// by the method alone.
	req, err := http.NewRequest("GET", "http://localhost/v1/nope", nil)
	c.Assert(err, IsNil)
	req, span := tracing.StartHTTPServerSpan(req, tracing.ServerTransportUnixSocket)
	req.Pattern = "/"
	tracing.EndHTTPServerSpan(span, req, tracing.HTTPServerResult{Status: 404})

	// Unknown methods are named "HTTP" to bound cardinality.
	req, err = http.NewRequest("FROB", "http://localhost/v1/changes/42", nil)
	c.Assert(err, IsNil)
	req, span = tracing.StartHTTPServerSpan(req, tracing.ServerTransportUnixSocket)
	req.Pattern = "/v1/changes/{id}"
	tracing.EndHTTPServerSpan(span, req, tracing.HTTPServerResult{Status: 405})

	ended := s.recorder.Ended()
	c.Assert(ended, HasLen, 2)
	c.Check(ended[0].Name(), Equals, "GET")
	for _, kv := range ended[0].Attributes() {
		c.Check(string(kv.Key), Not(Equals), "http.route")
	}
	c.Check(ended[1].Name(), Equals, "HTTP /v1/changes/{id}")
	attrs := make(map[string]any)
	for _, kv := range ended[1].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	c.Check(attrs["http.request.method"], Equals, "_OTHER")
	c.Check(attrs["http.request.method_original"], Equals, "FROB")
	c.Check(attrs["http.route"], Equals, "/v1/changes/{id}")
}
