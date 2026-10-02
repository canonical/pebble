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
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/GehirnInc/crypt/sha512_crypt"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/identities"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

type tracingSuite struct {
	recorder *tracingtest.Recorder

	// handlerSpan is the span context seen by the routed handler.
	handlerSpan tracing.SpanContext
	router      *http.ServeMux

	daemons []*Daemon
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	s.recorder = tracingtest.NewRecorder()

	s.handlerSpan = tracing.SpanContext{}
	s.router = http.NewServeMux()
	s.router.HandleFunc("/v1/changes/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.handlerSpan = tracing.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusAccepted)
	})
	s.router.HandleFunc("/v1/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	s.router.Handle("/", NotFound("invalid API endpoint requested"))
}

func (s *tracingSuite) TearDownTest(c *C) {
	for _, d := range s.daemons {
		func() {
			defer func() { _ = recover() }()
			_ = d.Overlord().Stop()
		}()
	}
	s.daemons = nil
	s.recorder.Restore()

	err := reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

// newDaemon returns a daemon whose overlord has been started up (so that its
// state and managers are ready), but which isn't serving requests or running
// changes.
func (s *tracingSuite) newDaemon(c *C) *Daemon {
	d, err := New(&Options{Dir: c.MkDir()})
	c.Assert(err, IsNil)
	d.addRoutes()
	c.Assert(d.overlord.StartUp(), IsNil)
	s.daemons = append(s.daemons, d)
	return d
}

func (s *tracingSuite) serve(c *C, req *http.Request) tracingtest.ReadOnlySpan {
	traceRequest(s.router).ServeHTTP(httptest.NewRecorder(), req)
	spans := s.recorder.Ended()
	c.Assert(spans, HasLen, 1)
	return spans[0]
}

// serveRouter serves req with router wrapped in traceRequest, as the daemon
// does, returning the server span for the request and the response. Other
// spans ended while handling the request (for example, an "authenticate"
// span) are left in the recorder.
func (s *tracingSuite) serveRouter(c *C, router http.Handler, req *http.Request) (tracingtest.ReadOnlySpan, *httptest.ResponseRecorder) {
	s.recorder.Reset()
	rec := httptest.NewRecorder()
	traceRequest(router).ServeHTTP(rec, req)
	var serverSpan tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.SpanKind() == tracing.SpanKindServer {
			c.Assert(serverSpan, IsNil, Commentf("more than one server span recorded"))
			serverSpan = span
		}
	}
	c.Assert(serverSpan, NotNil, Commentf("no server span recorded"))
	return serverSpan, rec
}

// findSpan returns the ended span with the given name, failing the test if
// there isn't exactly one.
func findSpan(c *C, spans []tracingtest.ReadOnlySpan, name string) tracingtest.ReadOnlySpan {
	var found tracingtest.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == name {
			c.Assert(found, IsNil, Commentf("more than one %q span recorded", name))
			found = span
		}
	}
	c.Assert(found, NotNil, Commentf("no %q span recorded (have %v)", name, spanNames(spans)))
	return found
}

func spanNames(spans []tracingtest.ReadOnlySpan) []string {
	var names []string
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func spanAttrs(span tracingtest.ReadOnlySpan) map[string]tracing.AttributeValue {
	attrs := make(map[string]tracing.AttributeValue)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
}

// eventNames returns the names of the span's events, in order.
func eventNames(span tracingtest.ReadOnlySpan) []string {
	var names []string
	for _, event := range span.Events() {
		names = append(names, event.Name)
	}
	return names
}

// unixRequest returns a request as received over the unix socket from a
// process with the given UID.
func unixRequest(c *C, method, url string, uid int) *http.Request {
	ctx := context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeUnixSocket)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=" + strconv.Itoa(uid) + ";socket=;"
	return req
}

// fakeHijacker is a response writer that can be hijacked.
type fakeHijacker struct {
	*httptest.ResponseRecorder
	hijacked bool
	err      error
}

func (h *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h.err != nil {
		return nil, nil, h.err
	}
	h.hijacked = true
	return nil, nil, nil
}

func (s *tracingSuite) TestServerSpan(c *C) {
	req := httptest.NewRequest("GET", "/v1/changes/42", nil)
	req = req.WithContext(context.WithValue(req.Context(), TransportTypeKey{}, TransportTypeUnixSocket))
	req.Header.Set("User-Agent", "pebble/1.0")
	span := s.serve(c, req)

	c.Check(span.Name(), Equals, "GET /v1/changes/{id}")
	c.Check(span.SpanKind(), Equals, tracing.SpanKindServer)
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
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
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	// Nothing recorded an error message, so the status text is used.
	c.Check(span.Status().Description, Equals, "Internal Server Error")
	c.Check(spanAttrs(span)["http.response.status_code"].AsInt64(), Equals, int64(500))
}

func (s *tracingSuite) TestServerSpanUnknownEndpoint(c *C) {
	span := s.serve(c, httptest.NewRequest("GET", "/v1/nope", nil))
	c.Check(span.Name(), Equals, "GET")
	_, hasRoute := spanAttrs(span)["http.route"]
	c.Check(hasRoute, Equals, false)
	c.Check(spanAttrs(span)["http.response.status_code"].AsInt64(), Equals, int64(404))
	// 4xx responses aren't server errors.
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
}

func (s *tracingSuite) TestServerSpanUnknownMethod(c *C) {
	span := s.serve(c, httptest.NewRequest("FROB", "/v1/changes/42", nil))
	c.Check(span.Name(), Equals, "HTTP /v1/changes/{id}")
	attrs := spanAttrs(span)
	c.Check(attrs["http.request.method"].AsString(), Equals, "_OTHER")
	c.Check(attrs["http.request.method_original"].AsString(), Equals, "FROB")
}

// fakeCommandRouter returns a router serving cmd at /v1/fake.
func fakeCommandRouter(cmd *Command) *http.ServeMux {
	router := http.NewServeMux()
	router.Handle("/v1/fake", cmd)
	return router
}

func (s *tracingSuite) TestCommandAsyncResponse(c *C) {
	cmd := &Command{d: s.newDaemon(c), WriteAccess: OpenAccess{}}
	cmd.POST = func(*Command, *http.Request, *UserState) Response {
		return AsyncResponse(nil, "42")
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "POST", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusAccepted)
	c.Check(span.Name(), Equals, "POST /v1/fake")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(202))
	c.Check(attrs["pebble.change.id"].AsString(), Equals, "42")
	_, hasErrorType := attrs["error.type"]
	c.Check(hasErrorType, Equals, false)
}

func (s *tracingSuite) TestCommandSyncResponse(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(map[string]any{"foo": "bar"})
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(200))
	for _, key := range []string{"pebble.change.id", "error.type", "pebble.error.message", "pebble.maintenance.kind"} {
		_, has := attrs[key]
		c.Check(has, Equals, false, Commentf("unexpected attribute %s", key))
	}
}

func (s *tracingSuite) TestCommandClientError(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return NotFound("cannot find thing %q", "x")
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusNotFound)
	// A 4xx response is the client's fault, so the span isn't an error, but
	// the error is still recorded. The error has no kind, so the status
	// code is used as the error type.
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(404))
	c.Check(attrs["error.type"].AsString(), Equals, "404")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, `cannot find thing "x"`)
}

func (s *tracingSuite) TestCommandErrorKind(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return &resp{
			Type:   ResponseTypeError,
			Status: http.StatusNotFound,
			Result: &errorResult{Kind: errorKindNotFound, Message: "no such file"},
		}
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusNotFound)
	attrs := spanAttrs(span)
	c.Check(attrs["error.type"].AsString(), Equals, "not-found")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "no such file")
}

func (s *tracingSuite) TestCommandServerError(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return ServerError("cannot frob: %v", "bad wiring")
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusInternalServerError)
	// The error message is the span's status description.
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, "cannot frob: bad wiring")
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(500))
	c.Check(attrs["error.type"].AsString(), Equals, "500")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "cannot frob: bad wiring")
}

func (s *tracingSuite) TestCommandUnauthorized(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: UserAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}
	// No credentials at all.
	ctx := context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeUnixSocket)
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/fake", nil)
	c.Assert(err, IsNil)
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), req)

	c.Check(rec.Code, Equals, http.StatusUnauthorized)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["error.type"].AsString(), Equals, "login-required")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "access denied")
	// No user was identified, so none is recorded.
	for _, key := range []string{"pebble.user.access", "user.name", "user.id"} {
		_, has := attrs[key]
		c.Check(has, Equals, false, Commentf("unexpected attribute %s", key))
	}

	// The authentication is a child span of the request.
	auth := findSpan(c, s.recorder.Ended(), "authenticate")
	c.Check(auth.Parent().SpanID(), Equals, span.SpanContext().SpanID())
	c.Check(auth.SpanContext().TraceID(), Equals, span.SpanContext().TraceID())
	authAttrs := spanAttrs(auth)
	c.Check(authAttrs["pebble.auth.method"].AsString(), Equals, "none")
	c.Check(authAttrs["pebble.auth.identified"].AsBool(), Equals, false)
}

func (s *tracingSuite) TestCommandDegradedMode(c *C) {
	d := s.newDaemon(c)
	d.SetDegradedMode(context.DeadlineExceeded)
	cmd := &Command{d: d, WriteAccess: OpenAccess{}}
	cmd.POST = func(*Command, *http.Request, *UserState) Response {
		c.Fatalf("handler should not be called in degraded mode")
		return nil
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "POST", "/v1/fake", 0))

	c.Check(rec.Code, Equals, http.StatusInternalServerError)
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, "context deadline exceeded")
	c.Check(spanAttrs(span)["pebble.error.message"].AsString(), Equals, "context deadline exceeded")
}

func (s *tracingSuite) TestCommandMaintenance(c *C) {
	d := s.newDaemon(c)
	cmd := &Command{d: d, ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}

	d.overlord.RestartManager().FakePending(restart.RestartSystem)
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(spanAttrs(span)["pebble.maintenance.kind"].AsString(), Equals, "system-restart")

	d.overlord.RestartManager().FakePending(restart.RestartDaemon)
	span, rec = s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(spanAttrs(span)["pebble.maintenance.kind"].AsString(), Equals, "daemon-restart")
	// Maintenance isn't an error.
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	_, hasErrorType := spanAttrs(span)["error.type"]
	c.Check(hasErrorType, Equals, false)
}

func (s *tracingSuite) TestCommandUserUcred(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: UserAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}

	// Root gets admin access by default.
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 0))
	c.Check(rec.Code, Equals, http.StatusOK)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "admin")
	c.Check(attrs["user.id"].AsString(), Equals, "0")
	_, hasName := attrs["user.name"]
	c.Check(hasName, Equals, false)

	auth := findSpan(c, s.recorder.Ended(), "authenticate")
	c.Check(auth.Parent().SpanID(), Equals, span.SpanContext().SpanID())
	authAttrs := spanAttrs(auth)
	c.Check(authAttrs["pebble.auth.method"].AsString(), Equals, "ucred")
	// Root isn't a named identity.
	c.Check(authAttrs["pebble.auth.identified"].AsBool(), Equals, false)

	// Other local users get read access by default.
	span, rec = s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 4242))
	c.Check(rec.Code, Equals, http.StatusOK)
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "read")
	c.Check(attrs["user.id"].AsString(), Equals, "4242")
}

func (s *tracingSuite) TestCommandUserIdentity(c *C) {
	d := s.newDaemon(c)
	hashedPassword, err := sha512_crypt.New().Generate([]byte("test"), nil)
	c.Assert(err, IsNil)
	d.state.Lock()
	err = d.overlord.IdentitiesManager().AddIdentities(map[string]*identities.Identity{
		"localuser": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
		"bob": {
			Access: identities.ReadAccess,
			Basic:  &identities.BasicIdentity{Password: hashedPassword},
		},
	})
	d.state.Unlock()
	c.Assert(err, IsNil)

	cmd := &Command{d: d, ReadAccess: UserAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}

	// A named local identity, identified by its UID.
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 1000))
	c.Check(rec.Code, Equals, http.StatusOK)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "admin")
	c.Check(attrs["user.name"].AsString(), Equals, "localuser")
	c.Check(attrs["user.id"].AsString(), Equals, "1000")
	auth := findSpan(c, s.recorder.Ended(), "authenticate")
	c.Check(auth.Parent().SpanID(), Equals, span.SpanContext().SpanID())
	c.Check(spanAttrs(auth)["pebble.auth.method"].AsString(), Equals, "ucred")
	c.Check(spanAttrs(auth)["pebble.auth.identified"].AsBool(), Equals, true)

	// A basic auth identity (the password is "test"), over the unix socket.
	req := unixRequest(c, "GET", "/v1/fake", 4242)
	req.SetBasicAuth("bob", "test")
	span, rec = s.serveRouter(c, fakeCommandRouter(cmd), req)
	c.Check(rec.Code, Equals, http.StatusOK)
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "read")
	c.Check(attrs["user.name"].AsString(), Equals, "bob")
	_, hasID := attrs["user.id"]
	c.Check(hasID, Equals, false)
	auth = findSpan(c, s.recorder.Ended(), "authenticate")
	c.Check(spanAttrs(auth)["pebble.auth.method"].AsString(), Equals, "basic")
	c.Check(spanAttrs(auth)["pebble.auth.identified"].AsBool(), Equals, true)

	// Basic auth with the wrong password: not identified, and the ucred
	// default applies instead.
	req = unixRequest(c, "GET", "/v1/fake", 4242)
	req.SetBasicAuth("bob", "wrong")
	span, rec = s.serveRouter(c, fakeCommandRouter(cmd), req)
	c.Check(rec.Code, Equals, http.StatusOK)
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "read")
	c.Check(attrs["user.id"].AsString(), Equals, "4242")
	auth = findSpan(c, s.recorder.Ended(), "authenticate")
	c.Check(spanAttrs(auth)["pebble.auth.method"].AsString(), Equals, "basic")
	c.Check(spanAttrs(auth)["pebble.auth.identified"].AsBool(), Equals, false)
}

func (s *tracingSuite) TestCommandOpenAccessNoAuthenticate(c *C) {
	cmd := &Command{d: s.newDaemon(c), ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}
	span, rec := s.serveRouter(c, fakeCommandRouter(cmd), unixRequest(c, "GET", "/v1/fake", 4242))
	c.Check(rec.Code, Equals, http.StatusOK)

	// Open endpoints don't look up identities (to avoid taking the state
	// lock), so there's no authenticate span, but the default ucred user is
	// still recorded.
	c.Check(spanNames(s.recorder.Ended()), DeepEquals, []string{"GET /v1/fake"})
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.user.access"].AsString(), Equals, "read")
	c.Check(attrs["user.id"].AsString(), Equals, "4242")
}

func (s *tracingSuite) newChange(d *Daemon) *state.Change {
	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	change := st.NewChange("exec", "Exec")
	task := st.NewTask("exec", "Exec")
	change.AddAll(state.NewTaskSet(task))
	return change
}

func (s *tracingSuite) TestWaitChangeTimeout(c *C) {
	d := s.newDaemon(c)
	change := s.newChange(d)

	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/changes/"+change.ID()+"/wait?timeout=10ms", 0))

	// Timing out is an expected outcome of the request, not a failure.
	c.Check(rec.Code, Equals, http.StatusGatewayTimeout)
	c.Check(span.Name(), Equals, "GET /v1/changes/{id}/wait")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(504))
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "timeout")
	c.Check(attrs["pebble.wait.timeout"].AsString(), Equals, "10ms")
	c.Check(attrs["pebble.change.id"].AsString(), Equals, change.ID())
	_, hasStatus := attrs["pebble.change.status"]
	c.Check(hasStatus, Equals, false)
	// The error is still recorded.
	c.Check(attrs["error.type"].AsString(), Equals, "504")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "timed out waiting for change after 10ms")
}

func (s *tracingSuite) TestWaitChangeReady(c *C) {
	d := s.newDaemon(c)
	change := s.newChange(d)
	st := d.overlord.State()

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		change.SetStatus(state.DoneStatus)
		st.Unlock()
	}()
	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/changes/"+change.ID()+"/wait?timeout=5s", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "ready")
	c.Check(attrs["pebble.wait.timeout"].AsString(), Equals, "5s")
	c.Check(attrs["pebble.change.id"].AsString(), Equals, change.ID())
	c.Check(attrs["pebble.change.status"].AsString(), Equals, "Done")
}

func (s *tracingSuite) TestWaitChangeNoTimeout(c *C) {
	d := s.newDaemon(c)
	change := s.newChange(d)
	st := d.overlord.State()

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		change.SetStatus(state.ErrorStatus)
		st.Unlock()
	}()
	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/changes/"+change.ID()+"/wait", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "ready")
	_, hasTimeout := attrs["pebble.wait.timeout"]
	c.Check(hasTimeout, Equals, false)
	c.Check(attrs["pebble.change.status"].AsString(), Equals, "Error")
}

func (s *tracingSuite) TestWaitChangeCancelled(c *C) {
	d := s.newDaemon(c)
	change := s.newChange(d)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	req := unixRequest(c, "GET", "/v1/changes/"+change.ID()+"/wait?timeout=5s", 0).WithContext(
		context.WithValue(ctx, TransportTypeKey{}, TransportTypeUnixSocket))
	span, rec := s.serveRouter(c, d.router, req)

	// The client going away isn't a failure of the daemon either.
	c.Check(rec.Code, Equals, http.StatusInternalServerError)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "cancelled")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "request cancelled")
}

func (s *tracingSuite) TestHealth(c *C) {
	d := s.newDaemon(c)

	checks := []*checkstate.CheckInfo{
		{Name: "chk1", Status: checkstate.CheckStatusUp},
		{Name: "chk2", Status: checkstate.CheckStatusDown},
	}
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return checks, nil
	})
	defer restore()

	// A check being down is reported with a 502, but that's the endpoint
	// doing its job, not a failure of the daemon.
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req = req.WithContext(context.WithValue(req.Context(), TransportTypeKey{}, TransportTypeHTTP))
	span, rec := s.serveRouter(c, d.router, req)
	c.Check(rec.Code, Equals, http.StatusBadGateway)
	c.Check(span.Name(), Equals, "GET /v1/health")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(502))
	c.Check(attrs["pebble.health.healthy"].AsBool(), Equals, false)
	// Health is open access, so no authentication happens.
	c.Check(spanNames(s.recorder.Ended()), DeepEquals, []string{"GET /v1/health"})

	checks[1].Status = checkstate.CheckStatusUp
	span, rec = s.serveRouter(c, d.router, req)
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	c.Check(spanAttrs(span)["pebble.health.healthy"].AsBool(), Equals, true)
}

func (s *tracingSuite) TestNoticesWaitTimeout(c *C) {
	d := s.newDaemon(c)

	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/notices?timeout=10ms", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	c.Check(span.Name(), Equals, "GET /v1/notices")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "timeout")
	c.Check(attrs["pebble.wait.timeout"].AsString(), Equals, "10ms")
	c.Check(attrs["pebble.notices.count"].AsInt64(), Equals, int64(0))
}

func (s *tracingSuite) TestNoticesWaitNotices(c *C) {
	d := s.newDaemon(c)
	st := d.overlord.State()

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		_, err := st.AddNotice(nil, state.CustomNotice, "a.b/1", nil)
		st.Unlock()
		c.Check(err, IsNil)
	}()
	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/notices?timeout=5s", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "notices")
	c.Check(attrs["pebble.wait.timeout"].AsString(), Equals, "5s")
	c.Check(attrs["pebble.notices.count"].AsInt64(), Equals, int64(1))
}

func (s *tracingSuite) TestNoticesNoWait(c *C) {
	d := s.newDaemon(c)
	st := d.overlord.State()
	st.Lock()
	_, err := st.AddNotice(nil, state.CustomNotice, "a.b/1", nil)
	c.Check(err, IsNil)
	_, err = st.AddNotice(nil, state.CustomNotice, "a.b/2", nil)
	c.Check(err, IsNil)
	st.Unlock()

	span, rec := s.serveRouter(c, d.router, unixRequest(c, "GET", "/v1/notices", 0))

	c.Check(rec.Code, Equals, http.StatusOK)
	attrs := spanAttrs(span)
	_, hasOutcome := attrs["pebble.wait.outcome"]
	c.Check(hasOutcome, Equals, false)
	_, hasTimeout := attrs["pebble.wait.timeout"]
	c.Check(hasTimeout, Equals, false)
	c.Check(attrs["pebble.notices.count"].AsInt64(), Equals, int64(2))
}

func (s *tracingSuite) TestNoticesWaitCancelled(c *C) {
	d := s.newDaemon(c)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	req := unixRequest(c, "GET", "/v1/notices?timeout=5s", 0).WithContext(
		context.WithValue(ctx, TransportTypeKey{}, TransportTypeUnixSocket))
	span, rec := s.serveRouter(c, d.router, req)

	c.Check(rec.Code, Equals, http.StatusBadRequest)
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.wait.outcome"].AsString(), Equals, "cancelled")
	c.Check(attrs["pebble.error.message"].AsString(), Equals, "request canceled")
}

func (s *tracingSuite) TestWrappedWriterHijack(c *C) {
	// A websocket handshake writes its response directly to the hijacked
	// connection, so the wrapped writer records the status it will send.
	hijacker := &fakeHijacker{ResponseRecorder: httptest.NewRecorder()}
	ww := &wrappedWriter{w: hijacker}
	_, _, err := ww.Hijack()
	c.Assert(err, IsNil)
	c.Check(hijacker.hijacked, Equals, true)
	c.Check(ww.status(), Equals, http.StatusSwitchingProtocols)

	// A status written before hijacking is kept.
	hijacker = &fakeHijacker{ResponseRecorder: httptest.NewRecorder()}
	ww = &wrappedWriter{w: hijacker}
	ww.WriteHeader(http.StatusBadRequest)
	_, _, err = ww.Hijack()
	c.Assert(err, IsNil)
	c.Check(ww.status(), Equals, http.StatusBadRequest)

	// A failed hijack doesn't record a status.
	hijacker = &fakeHijacker{ResponseRecorder: httptest.NewRecorder(), err: context.Canceled}
	ww = &wrappedWriter{w: hijacker}
	_, _, err = ww.Hijack()
	c.Assert(err, Equals, context.Canceled)
	c.Check(ww.status(), Equals, http.StatusOK)

	// A writer that can't be hijacked.
	ww = &wrappedWriter{w: httptest.NewRecorder()}
	_, _, err = ww.Hijack()
	c.Assert(err, ErrorMatches, "underlying writer does not implement Hijack")
}
