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
	"strconv"

	"github.com/canonical/pebble/internals/tracing"
)

// Span attribute names used by the daemon. The full attribute key is prefixed
// with the program name (see tracing.AttrKey).
const (
	attrChangeID       = "change.id"
	attrChangeStatus   = "change.status"
	attrTaskID         = "task.id"
	attrWebsocketID    = "websocket.id"
	attrUserAccess     = "user.access"
	attrError          = "error.message"
	attrMaintenance    = "maintenance.kind"
	attrWaitTimeout    = "wait.timeout"
	attrWaitOutcome    = "wait.outcome"
	attrNoticesCount   = "notices.count"
	attrHealthy        = "health.healthy"
	attrAuthMethod     = "auth.method"
	attrAuthIdentified = "auth.identified"
)

// requestTrace holds what handling a request records for its server span
// beyond the response status code. It's carried by the request context.
type requestTrace struct {
	errorMessage string
	expected     bool
}

type requestTraceKey struct{}

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
		rt := &requestTrace{}
		r = r.WithContext(context.WithValue(r.Context(), requestTraceKey{}, rt))
		ww := &wrappedWriter{w: w}
		handler.ServeHTTP(ww, r)
		tracing.EndHTTPServerSpan(span, r, tracing.HTTPServerResult{
			Status:       ww.status(),
			ErrorMessage: rt.errorMessage,
			Expected:     rt.expected,
		})
	})
}

// expectedStatus marks the status of the response to r as an expected
// outcome of the request, so that a 5xx status (such as a long poll timing
// out) isn't recorded as an error on the request's span.
func expectedStatus(r *http.Request) {
	if rt, ok := r.Context().Value(requestTraceKey{}).(*requestTrace); ok {
		rt.expected = true
	}
}

// traceUser records the user making the request on the request's span.
func traceUser(r *http.Request, user *UserState) {
	if user == nil {
		return
	}
	span := tracing.SpanFromContext(r.Context())
	span.SetAttributes(tracing.AttrKey(attrUserAccess).String(string(user.Access)))
	if user.Username != "" {
		span.SetAttributes(tracing.UserName(user.Username))
	}
	if user.UID != nil {
		span.SetAttributes(tracing.UserID(strconv.FormatUint(uint64(*user.UID), 10)))
	}
}

// traceResponse records the response to r on the request's span: the change
// it created, if any, and the kind and message of an error response. It
// must be called before the response is written.
func traceResponse(r *http.Request, rsp Response) {
	resp, ok := rsp.(*resp)
	if !ok {
		return
	}
	span := tracing.SpanFromContext(r.Context())
	if resp.Change != "" {
		span.SetAttributes(tracing.AttrKey(attrChangeID).String(resp.Change))
	}
	if resp.Maintenance != nil {
		span.SetAttributes(tracing.AttrKey(attrMaintenance).String(string(resp.Maintenance.Kind)))
	}
	if resp.Type != ResponseTypeError {
		return
	}
	result, ok := resp.Result.(*errorResult)
	if !ok {
		return
	}
	// The error kind is the class of error, falling back to the status code
	// as the HTTP semantic conventions suggest.
	kind := string(result.Kind)
	if kind == "" {
		kind = strconv.Itoa(resp.Status)
	}
	span.SetAttributes(
		tracing.ErrorType(kind),
		tracing.AttrKey(attrError).String(result.Message),
	)
	if rt, ok := r.Context().Value(requestTraceKey{}).(*requestTrace); ok {
		rt.errorMessage = result.Message
	}
}
