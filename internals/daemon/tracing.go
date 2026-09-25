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
	"net"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/canonical/pebble/internals/tracing"
)

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

// traceRequest wraps handler, which must be the API router, to create a
// server span for each API request. The span is a child of the span described
// by the request's W3C Trace Context headers (traceparent and tracestate), if
// any, and is carried by the request context, so that changes created by the
// request are part of the same trace.
func traceRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		switch RequestTransportType(r) {
		case TransportTypeUnixSocket:
			attrs = append(attrs, semconv.NetworkTransportUnix, semconv.URLScheme("http"))
		case TransportTypeHTTP, TransportTypeHTTPS:
			attrs = append(attrs, semconv.NetworkTransportTCP, semconv.URLScheme(RequestTransportType(r).String()))
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				attrs = append(attrs, semconv.ClientAddress(host))
			}
		}
		if ua := r.UserAgent(); ua != "" {
			attrs = append(attrs, semconv.UserAgentOriginal(ua))
		}

		ctx, span := tracing.Tracer().Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		ww := &wrappedWriter{w: w}
		r = r.WithContext(ctx)
		handler.ServeHTTP(ww, r)

		// The router records the matched pattern on the request. The "/"
		// pattern is the catch-all for unknown endpoints, which isn't a
		// meaningful route.
		if r.Pattern != "" && r.Pattern != "/" {
			span.SetName(spanName + " " + r.Pattern)
			span.SetAttributes(semconv.HTTPRoute(r.Pattern))
		}

		status := ww.status()
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		// For server spans, only 5xx responses are errors; 4xx responses
		// are the client's fault.
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	})
}
