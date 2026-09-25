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

package checkstate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/testutil"
)

type tracingSuite struct {
	testutil.BaseTest
	recorder *tracetest.SpanRecorder
	tp       *sdktrace.TracerProvider
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.recorder = tracetest.NewSpanRecorder()
	s.tp = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.recorder))
	restore := otel.GetTracerProvider()
	otel.SetTracerProvider(s.tp)
	s.AddCleanup(func() { otel.SetTracerProvider(restore) })
}

// startSpan starts a span to act as a check task's or request's span.
func (s *tracingSuite) startSpan(name string) (context.Context, trace.Span) {
	return s.tp.Tracer("test").Start(context.Background(), name)
}

func (s *tracingSuite) endedSpan(c *C, name string) sdktrace.ReadOnlySpan {
	for _, span := range s.recorder.Ended() {
		if span.Name() == name {
			return span
		}
	}
	c.Fatalf("no ended span named %q", name)
	return nil
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
}

func testCheckConfig() *plan.Check {
	return &plan.Check{
		Name:    "chk",
		Level:   plan.ReadyLevel,
		Timeout: plan.OptionalDuration{Value: time.Second},
		TCP:     &plan.TCPCheck{Port: 1},
	}
}

func (s *tracingSuite) TestPeriodicRunIsNewRootLinkedToTask(c *C) {
	taskCtx, taskSpan := s.startSpan("task")
	defer taskSpan.End()

	ctx, checkSpan := startCheckSpan(taskCtx, nil, testCheckConfig())
	checkSpan.End()

	span := s.endedSpan(c, "check chk")
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Check(span.SpanContext().TraceID(), Not(Equals), taskSpan.SpanContext().TraceID())
	c.Assert(span.Links(), HasLen, 1)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, taskSpan.SpanContext().SpanID())
	c.Check(span.Status().Code, Equals, codes.Unset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.check.name"].AsString(), Equals, "chk")
	c.Check(attrs["pebble.check.type"].AsString(), Equals, "tcp")
	c.Check(attrs["pebble.check.level"].AsString(), Equals, "ready")

	// The returned context carries the check's span, and still the task's
	// cancellation.
	c.Check(trace.SpanContextFromContext(ctx).SpanID(), Equals, span.SpanContext().SpanID())
}

func (s *tracingSuite) TestRefreshRunIsChildOfRequest(c *C) {
	taskCtx, taskSpan := s.startSpan("task")
	defer taskSpan.End()
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()

	_, checkSpan := startCheckSpan(taskCtx, requestCtx, testCheckConfig())
	checkSpan.End()

	span := s.endedSpan(c, "check chk")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
	c.Assert(span.Links(), HasLen, 1)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, taskSpan.SpanContext().SpanID())
}

func (s *tracingSuite) TestStoppedCheckRefreshNotLinkedToItself(c *C) {
	// When a stopped check is refreshed, the request context is both the
	// parent and the context the check runs in.
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()

	_, checkSpan := startCheckSpan(requestCtx, requestCtx, testCheckConfig())
	checkSpan.End()

	span := s.endedSpan(c, "check chk")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.Links(), HasLen, 0)
}

func (s *tracingSuite) TestHTTPCheckerSpan(c *C) {
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	ctx, parent := s.startSpan("check")
	defer parent.End()

	u := strings.Replace(server.URL, "http://", "http://user:secret@", 1) + "/ok?token=abc"
	chk := &httpChecker{name: "chk", url: u}
	err := chk.check(ctx)
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "GET")
	c.Check(span.SpanKind(), Equals, trace.SpanKindClient)
	c.Check(span.Parent().SpanID(), Equals, parent.SpanContext().SpanID())
	attrs := spanAttrs(span)
	c.Check(attrs["http.request.method"].AsString(), Equals, "GET")
	c.Check(attrs["url.full"].AsString(), Equals, strings.Replace(server.URL, "http://", "http://user:xxxxx@", 1)+"/ok?token=REDACTED")
	c.Check(attrs["server.address"].AsString(), Equals, "127.0.0.1")
	c.Check(attrs["server.port"].AsInt64(), Not(Equals), int64(0))
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(200))
	c.Check(span.Status().Code, Equals, codes.Unset)

	// The trace is propagated to the checked service, from the client span.
	c.Check(headers.Get("traceparent"), Equals,
		"00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01")

	// A non-2xx response is an error.
	s.recorder.Reset()
	chk = &httpChecker{name: "chk", url: server.URL + "/fail"}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, "non-2xx status code 503")
	span = s.endedSpan(c, "GET")
	c.Check(span.Status().Code, Equals, codes.Error)
	c.Check(spanAttrs(span)["http.response.status_code"].AsInt64(), Equals, int64(503))
}

func (s *tracingSuite) TestHTTPCheckerConfiguredTraceparentWins(c *C) {
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header
	}))
	defer server.Close()

	ctx, parent := s.startSpan("check")
	defer parent.End()

	configured := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	chk := &httpChecker{url: server.URL, headers: map[string]string{"traceparent": configured}}
	c.Assert(chk.check(ctx), IsNil)
	c.Check(headers.Get("traceparent"), Equals, configured)
}

func (s *tracingSuite) TestTCPCheckerAttributes(c *C) {
	ctx, span := s.startSpan("check")
	chk := &tcpChecker{host: "127.0.0.1", port: 1}
	chk.check(ctx) // Port 1 is almost certainly closed; the result doesn't matter.
	span.End()

	attrs := spanAttrs(s.endedSpan(c, "check"))
	c.Check(attrs["network.transport"].AsString(), Equals, "tcp")
	c.Check(attrs["server.address"].AsString(), Equals, "127.0.0.1")
	c.Check(attrs["server.port"].AsInt64(), Equals, int64(1))
}

func (s *tracingSuite) TestExecCheckerTraceEnv(c *C) {
	c.Assert(reaper.Start(), IsNil)
	defer reaper.Stop()

	// The daemon's own trace context isn't inherited.
	s.Setenv("TRACEPARENT", "00-11111111111111111111111111111111-2222222222222222-01")

	ctx, span := s.startSpan("check")
	chk := &execChecker{command: `/bin/sh -c 'echo "$TRACEPARENT"; exit 1'`}
	err := chk.check(ctx)
	span.End()
	c.Assert(err, ErrorMatches, "exit status 1")
	c.Check(err.(*detailsError).Details(), Equals,
		"00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01")

	attrs := spanAttrs(s.endedSpan(c, "check"))
	c.Check(attrs["process.executable.name"].AsString(), Equals, "sh")
	c.Check(attrs["process.pid"].AsInt64(), Not(Equals), int64(0))
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(1))

	// A trace context configured for the check takes precedence.
	configured := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	chk = &execChecker{
		command:     `/bin/sh -c 'echo "$TRACEPARENT"; exit 1'`,
		environment: map[string]string{"TRACEPARENT": configured},
	}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, "exit status 1")
	c.Check(err.(*detailsError).Details(), Equals, configured)
}

// checkpointBackend is a state backend that always checkpoints.
type checkpointBackend struct{}

func (checkpointBackend) Checkpoint([]byte) error    { return nil }
func (checkpointBackend) EnsureBefore(time.Duration) {}
func (checkpointBackend) NeedsCheckpoint() bool      { return true }

func (s *tracingSuite) TestCheckpointInCheckTrace(c *C) {
	c.Assert(reaper.Start(), IsNil)
	defer reaper.Stop()

	st := state.New(checkpointBackend{})
	runner := state.NewTaskRunner(st)
	planMgr, err := planstate.NewManager(c.MkDir())
	c.Assert(err, IsNil)
	mgr := NewManager(st, runner, planMgr)
	mgr.PlanChanged(&plan.Plan{Checks: map[string]*plan.Check{
		"chk": {
			Name:      "chk",
			Override:  "replace",
			Period:    plan.OptionalDuration{Value: 10 * time.Millisecond},
			Timeout:   plan.OptionalDuration{Value: time.Second},
			Threshold: 3,
			Exec:      &plan.ExecCheck{Command: "true"},
		},
	}})
	// Start the perform-check task.
	c.Assert(runner.Ensure(), IsNil)
	defer runner.Stop()

	// Recording a check's result checkpoints the state, which should be
	// traced as part of the check run.
	for start := time.Now(); time.Since(start) < 10*time.Second; time.Sleep(10 * time.Millisecond) {
		checkSpans := make(map[trace.SpanID]sdktrace.ReadOnlySpan)
		var checkpoints []sdktrace.ReadOnlySpan
		for _, span := range s.recorder.Ended() {
			switch span.Name() {
			case "check chk":
				checkSpans[span.SpanContext().SpanID()] = span
			case "state checkpoint":
				checkpoints = append(checkpoints, span)
			}
		}
		for _, checkpoint := range checkpoints {
			checkSpan, ok := checkSpans[checkpoint.Parent().SpanID()]
			if !ok {
				continue
			}
			c.Check(checkpoint.SpanContext().TraceID(), Equals, checkSpan.SpanContext().TraceID())
			// The long-running perform-check task's span, whose task
			// data was modified, is linked rather than the parent.
			c.Assert(checkpoint.Links(), HasLen, 1)
			c.Check(checkpoint.Links()[0].SpanContext, DeepEquals, checkSpan.Links()[0].SpanContext)
			return
		}
	}
	c.Fatalf("timed out waiting for a checkpoint in a check's trace")
}
