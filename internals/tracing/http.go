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
	"net"
	"net/http"
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// InjectHTTPHeaders sets the W3C Trace Context headers (traceparent and
// tracestate) in header to describe the span carried by ctx, if any.
func InjectHTTPHeaders(ctx context.Context, header http.Header) {
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(header))
}

// knownMethods are the HTTP methods that may appear in span names and the
// http.request.method attribute. Others are recorded as "_OTHER" to bound
// cardinality, as the HTTP semantic conventions require.
var knownMethods = map[string]bool{
	http.MethodConnect: true,
	http.MethodDelete:  true,
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodPatch:   true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodTrace:   true,
}

// ServerTransport describes how an inbound HTTP request was received.
type ServerTransport int

const (
	ServerTransportUnknown ServerTransport = iota
	ServerTransportUnixSocket
	ServerTransportHTTP
	ServerTransportHTTPS
)

// StartHTTPServerSpan starts a server span for an inbound HTTP request. The
// span is a child of the span described by the request's W3C Trace Context
// headers (traceparent and tracestate), if any. It returns a copy of r
// carrying the span's context, which should be passed to the handler.
func StartHTTPServerSpan(r *http.Request, transport ServerTransport) (*http.Request, Span) {
	ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	spanName := r.Method
	var attrs []attribute.KeyValue
	if knownMethods[r.Method] {
		attrs = append(attrs, semconv.HTTPRequestMethodKey.String(r.Method))
	} else {
		spanName = "HTTP"
		attrs = append(attrs, semconv.HTTPRequestMethodOther, semconv.HTTPRequestMethodOriginal(r.Method))
	}
	attrs = append(attrs, semconv.URLPath(r.URL.Path))
	switch transport {
	case ServerTransportUnixSocket:
		attrs = append(attrs, semconv.NetworkTransportUnix, semconv.URLScheme("http"))
	case ServerTransportHTTP, ServerTransportHTTPS:
		scheme := "http"
		if transport == ServerTransportHTTPS {
			scheme = "https"
		}
		attrs = append(attrs, semconv.NetworkTransportTCP, semconv.URLScheme(scheme))
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			attrs = append(attrs, semconv.ClientAddress(host))
		}
	}
	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(ua))
	}

	ctx, span := Tracer().Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
	return r.WithContext(ctx), span
}

// EndHTTPServerSpan records the outcome of an inbound HTTP request on a span
// started with StartHTTPServerSpan, and ends it. The request r is the one
// returned by StartHTTPServerSpan, after it has been handled, and status is
// the response status code.
func EndHTTPServerSpan(span Span, r *http.Request, status int) {
	// The router records the matched pattern on the request. The "/"
	// pattern is the catch-all for unknown endpoints, which isn't a
	// meaningful route.
	if r.Pattern != "" && r.Pattern != "/" {
		method := r.Method
		if !knownMethods[method] {
			method = "HTTP"
		}
		span.SetName(method + " " + r.Pattern)
		span.SetAttributes(semconv.HTTPRoute(r.Pattern))
	}

	span.SetAttributes(semconv.HTTPResponseStatusCode(status))
	// For server spans, only 5xx responses are errors; 4xx responses
	// are the client's fault.
	if status >= 500 {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
	span.End()
}

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
