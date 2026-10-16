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
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/pebble/internals/tracing"
)

type tracingSuite struct {
	testutil.BaseTest
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) TestSpanContextFromEnv(c *C) {
	s.Setenv("TRACEPARENT", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	s.Setenv("TRACESTATE", "foo=bar")
	sc := tracing.SpanContextFromEnv()
	c.Assert(sc.IsValid(), Equals, true)
	c.Check(sc.TraceID().String(), Equals, "0af7651916cd43dd8448eb211c80319c")
	c.Check(sc.SpanID().String(), Equals, "b7ad6b7169203331")
	c.Check(sc.IsSampled(), Equals, true)
	c.Check(sc.TraceState().String(), Equals, "foo=bar")
	c.Check(sc.IsRemote(), Equals, true)
}

func (s *tracingSuite) TestSpanContextFromEnvUnset(c *C) {
	s.Setenv("TRACEPARENT", "")
	s.Setenv("TRACESTATE", "foo=bar")
	c.Check(tracing.SpanContextFromEnv().IsValid(), Equals, false)
}

func (s *tracingSuite) TestSpanContextFromEnvInvalid(c *C) {
	s.Setenv("TRACEPARENT", "garbage")
	c.Check(tracing.SpanContextFromEnv().IsValid(), Equals, false)
}

func (s *tracingSuite) TestSetupDisabled(c *C) {
	s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	s.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := tracing.Setup(context.Background())
	c.Assert(err, IsNil)
	c.Check(shutdown(context.Background()), IsNil)
}

func (s *tracingSuite) TestSetupShutdown(c *C) {
	// Setup installs a global tracer provider; put the previous one back
	// for the other tests.
	s.AddCleanup(func(tp trace.TracerProvider) func() {
		return func() { otel.SetTracerProvider(tp) }
	}(otel.GetTracerProvider()))

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		requests <- struct{}{}
	}))
	defer server.Close()
	s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	s.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	shutdown, err := tracing.Setup(context.Background())
	c.Assert(err, IsNil)
	_, span := tracing.Tracer().Start(context.Background(), "test")
	span.End()

	// Shutting down exports the span before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.Check(shutdown(ctx), IsNil)
	select {
	case <-requests:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for export")
	}
}

func (s *tracingSuite) TestSetupShutdownBlocked(c *C) {
	s.AddCleanup(func(tp trace.TracerProvider) func() {
		return func() { otel.SetTracerProvider(tp) }
	}(otel.GetTracerProvider()))

	// The collector reads the request but never responds, so the final
	// export can't finish.
	cancelled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		cancelled <- struct{}{}
	}))
	defer server.Close()
	s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	s.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	// Much longer than shutdown is given, so retrying doesn't hold it up.
	s.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "60000")

	shutdown, err := tracing.Setup(context.Background())
	c.Assert(err, IsNil)
	_, span := tracing.Tracer().Start(context.Background(), "test")
	span.End()

	// Shutdown gives up when its context is done, abandoning the export
	// rather than leaving it retrying in the background.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	c.Check(shutdown(ctx), ErrorMatches, "context deadline exceeded")
	c.Check(time.Since(start) < 5*time.Second, Equals, true)
	select {
	case <-cancelled:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for export to be abandoned")
	}
}

func (s *tracingSuite) TestAttrKey(c *C) {
	c.Check(string(tracing.AttrKey("change.id")), Equals, "pebble.change.id")

	old := cmd.ProgramName
	cmd.ProgramName = "foo"
	defer func() { cmd.ProgramName = old }()
	c.Check(string(tracing.AttrKey("change.id")), Equals, "foo.change.id")
}

func (s *tracingSuite) TestInjectEnv(c *C) {
	ts, err := trace.ParseTraceState("foo=bar")
	c.Assert(err, IsNil)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		TraceState: ts,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	env := map[string]string{"FOO": "bar"}
	tracing.InjectEnv(ctx, env)
	c.Check(env, DeepEquals, map[string]string{
		"FOO":         "bar",
		"TRACEPARENT": "00-01000000000000000000000000000000-0200000000000000-01",
		"TRACESTATE":  "foo=bar",
	})

	// An explicitly set TRACEPARENT is kept.
	env = map[string]string{"TRACEPARENT": "explicit"}
	tracing.InjectEnv(ctx, env)
	c.Check(env, DeepEquals, map[string]string{"TRACEPARENT": "explicit"})

	// Nothing is set without a span.
	env = map[string]string{}
	tracing.InjectEnv(context.Background(), env)
	c.Check(env, HasLen, 0)
}

func (s *tracingSuite) TestDeleteEnv(c *C) {
	env := map[string]string{"FOO": "bar", "TRACEPARENT": "x", "TRACESTATE": "y"}
	tracing.DeleteEnv(env)
	c.Check(env, DeepEquals, map[string]string{"FOO": "bar"})
}

func (s *tracingSuite) TestEndSpan(c *C) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	_, span := tracer.Start(context.Background(), "ok")
	tracing.EndSpan(span, nil)
	_, span = tracer.Start(context.Background(), "failed")
	tracing.EndSpan(span, errors.New("boom"))

	spans := recorder.Ended()
	c.Assert(spans, HasLen, 2)
	c.Check(spans[0].Status().Code, Equals, codes.Unset)
	c.Check(spans[0].Events(), HasLen, 0)
	c.Check(spans[1].Status().Code, Equals, codes.Error)
	c.Check(spans[1].Status().Description, Equals, "boom")
	c.Assert(spans[1].Events(), HasLen, 1)
	c.Check(spans[1].Events()[0].Name, Equals, "exception")
}
