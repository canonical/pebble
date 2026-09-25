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
	"net/http"
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// StartHTTPClientSpan starts a client span for an outbound HTTP request, as a
// child of the span carried by the request's context, and propagates the
// trace to the server by setting the W3C Trace Context headers (traceparent
// and tracestate). A traceparent header already set on req, for example from
// configuration, takes precedence. It returns a copy of req carrying the
// span's context, which should be used to make the request.
func StartHTTPClientSpan(req *http.Request) (*http.Request, trace.Span) {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFull(RedactURL(req.URL.String())),
		semconv.ServerAddress(req.URL.Hostname()),
	}
	if port, err := strconv.Atoi(req.URL.Port()); err == nil {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	ctx, span := Tracer().Start(req.Context(), req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	req = req.WithContext(ctx)
	if req.Header.Get("traceparent") == "" {
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	}
	return req, span
}

// EndHTTPClientSpan records the outcome of an outbound HTTP request on a span
// started with StartHTTPClientSpan, and ends it. The response may be nil if
// the request failed. Following the HTTP semantic conventions for client
// spans, a 4xx or 5xx response is an error.
func EndHTTPClientSpan(span trace.Span, resp *http.Response, err error) {
	if resp != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))
		if err == nil && resp.StatusCode >= 400 {
			span.SetStatus(codes.Error, resp.Status)
		}
	}
	EndSpan(span, err)
}

// RedactURL returns rawURL with any password and query parameter values
// redacted, as they may contain credentials. It returns "" if rawURL can't
// be parsed.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if u.RawQuery != "" {
		query := u.Query()
		for k := range query {
			query[k] = []string{"REDACTED"}
		}
		u.RawQuery = query.Encode()
	}
	return u.Redacted()
}
