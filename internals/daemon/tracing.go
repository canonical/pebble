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
	"net/http"

	"github.com/canonical/pebble/internals/tracing"
)

// traceRequest wraps handler, which must be the API router, to create a
// server span for each API request. The span is a child of the span described
// by the request's W3C Trace Context headers (traceparent and tracestate), if
// any, and is carried by the request context, so that changes created by the
// request are part of the same trace.
func traceRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var transport tracing.ServerTransport
		switch RequestTransportType(r) {
		case TransportTypeUnixSocket:
			transport = tracing.ServerTransportUnixSocket
		case TransportTypeHTTP:
			transport = tracing.ServerTransportHTTP
		case TransportTypeHTTPS:
			transport = tracing.ServerTransportHTTPS
		}
		r, span := tracing.StartHTTPServerSpan(r, transport)
		ww := &wrappedWriter{w: w}
		handler.ServeHTTP(ww, r)
		tracing.EndHTTPServerSpan(span, r, ww.status())
	})
}
