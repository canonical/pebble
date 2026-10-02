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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

type tracingSuite struct {
	testutil.BaseTest
	recorder *tracingtest.Recorder
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.recorder = tracingtest.NewRecorder()
	s.AddCleanup(s.recorder.Restore)
}

// startSpan starts a span to act as a check task's or request's span.
func (s *tracingSuite) startSpan(name string) (context.Context, tracing.Span) {
	return tracing.Tracer().Start(context.Background(), name)
}

func (s *tracingSuite) endedSpan(c *C, name string) tracingtest.ReadOnlySpan {
	for _, span := range s.recorder.Ended() {
		if span.Name() == name {
			return span
		}
	}
	c.Fatalf("no ended span named %q", name)
	return nil
}

func spanAttrs(span tracingtest.ReadOnlySpan) map[string]tracing.AttributeValue {
	attrs := make(map[string]tracing.AttributeValue)
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
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.check.name"].AsString(), Equals, "chk")
	c.Check(attrs["pebble.check.type"].AsString(), Equals, "tcp")
	c.Check(attrs["pebble.check.level"].AsString(), Equals, "ready")

	// The returned context carries the check's span, and still the task's
	// cancellation.
	c.Check(tracing.SpanContextFromContext(ctx).SpanID(), Equals, span.SpanContext().SpanID())
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
	c.Check(span.SpanKind(), Equals, tracing.SpanKindClient)
	c.Check(span.Parent().SpanID(), Equals, parent.SpanContext().SpanID())
	attrs := spanAttrs(span)
	c.Check(attrs["http.request.method"].AsString(), Equals, "GET")
	c.Check(attrs["url.full"].AsString(), Equals, strings.Replace(server.URL, "http://", "http://user:xxxxx@", 1)+"/ok?token=REDACTED")
	c.Check(attrs["server.address"].AsString(), Equals, "127.0.0.1")
	c.Check(attrs["server.port"].AsInt64(), Not(Equals), int64(0))
	c.Check(attrs["http.response.status_code"].AsInt64(), Equals, int64(200))
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)

	// The trace is propagated to the checked service, from the client span.
	c.Check(headers.Get("traceparent"), Equals,
		"00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01")

	// A non-2xx response is an error.
	s.recorder.Reset()
	chk = &httpChecker{name: "chk", url: server.URL + "/fail"}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, "non-2xx status code 503")
	span = s.endedSpan(c, "GET")
	c.Check(span.Status().Code, Equals, tracing.StatusError)
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
	mgr.PlanChanged(context.Background(), &plan.Plan{Checks: map[string]*plan.Check{
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
		checkSpans := make(map[tracing.SpanID]tracingtest.ReadOnlySpan)
		var checkpoints []tracingtest.ReadOnlySpan
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

// checkManagerEnv is a check manager with the state, task runner and plan
// manager it was created with, for tests that drive the manager directly
// (rather than through an overlord, which this package can't import).
type checkManagerEnv struct {
	st      *state.State
	runner  *state.TaskRunner
	planMgr *planstate.PlanManager
	mgr     *CheckManager
}

func (s *tracingSuite) newCheckManager(c *C) *checkManagerEnv {
	e := &checkManagerEnv{}
	e.st = state.New(checkpointBackend{})
	e.runner = state.NewTaskRunner(e.st)
	s.AddCleanup(e.runner.Stop)
	var err error
	e.planMgr, err = planstate.NewManager(c.MkDir())
	c.Assert(err, IsNil)
	e.mgr = NewManager(e.st, e.runner, e.planMgr)
	e.planMgr.AddChangeListener(e.mgr.PlanChanged)
	return e
}

// ensureUntil runs ensure passes until cond, called with the state locked,
// returns true.
func (e *checkManagerEnv) ensureUntil(c *C, what string, cond func() bool) {
	for start := time.Now(); time.Since(start) < 10*time.Second; time.Sleep(10 * time.Millisecond) {
		c.Assert(e.runner.Ensure(), IsNil)
		e.st.Lock()
		ok := cond()
		e.st.Unlock()
		if ok {
			return
		}
	}
	c.Fatalf("timed out waiting for %s", what)
}

// checkInfo returns the manager's information about the named check.
func (e *checkManagerEnv) checkInfo(c *C, name string) *CheckInfo {
	infos, err := e.mgr.Checks()
	c.Assert(err, IsNil)
	for _, info := range infos {
		if info.Name == name {
			return info
		}
	}
	c.Fatalf("no check named %q", name)
	return nil
}

// changeSpan returns the ended span of the change with the given ID.
func (s *tracingSuite) changeSpan(c *C, changeID string) tracingtest.ReadOnlySpan {
	for _, span := range s.recorder.Ended() {
		if strings.HasPrefix(span.Name(), "change ") && spanAttrs(span)["pebble.change.id"].AsString() == changeID {
			return span
		}
	}
	c.Fatalf("no ended span for change %s", changeID)
	return nil
}

// checkpointsCausedBy returns the ended "state checkpoint" spans whose
// parent is sc.
func (s *tracingSuite) checkpointsCausedBy(sc tracing.SpanContext) []tracingtest.ReadOnlySpan {
	var spans []tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == "state checkpoint" && span.Parent().SpanID() == sc.SpanID() {
			spans = append(spans, span)
		}
	}
	return spans
}

func checksLayer(label string, startup plan.CheckStartup, names ...string) *plan.Layer {
	layer := &plan.Layer{Label: label, Checks: make(map[string]*plan.Check)}
	for _, name := range names {
		layer.Checks[name] = &plan.Check{
			Name:      name,
			Override:  plan.ReplaceOverride,
			Period:    plan.OptionalDuration{Value: time.Second},
			Timeout:   plan.OptionalDuration{Value: time.Second},
			Threshold: 3,
			Startup:   startup,
			Exec:      &plan.ExecCheck{Command: "true"},
		}
	}
	return layer
}

func (s *tracingSuite) TestStartChecksChangeSpans(c *C) {
	e := s.newCheckManager(c)
	c.Assert(e.planMgr.AppendLayer(context.Background(), checksLayer("layer1", plan.CheckStartupDisabled, "chk1", "chk2"), false), IsNil)
	s.recorder.Reset()

	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()
	started, err := e.mgr.StartChecks(requestCtx, []string{"chk1", "chk2"})
	c.Assert(err, IsNil)
	c.Assert(started, DeepEquals, []string{"chk1", "chk2"})

	// Starting the checks checkpointed the state on behalf of the request.
	checkpoints := s.checkpointsCausedBy(requestSpan.SpanContext())
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())

	// The changes are children of the request. Aborting them, before they
	// have run, ends their spans.
	var changeIDs []string
	for _, name := range []string{"chk1", "chk2"} {
		changeID := e.checkInfo(c, name).ChangeID
		c.Assert(changeID, Not(Equals), "")
		changeIDs = append(changeIDs, changeID)
	}
	e.st.Lock()
	for _, changeID := range changeIDs {
		e.st.Change(changeID).Abort()
	}
	e.st.Unlock()
	for _, changeID := range changeIDs {
		span := s.changeSpan(c, changeID)
		c.Check(span.Name(), Equals, "change perform-check")
		c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
		c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
		c.Check(span.Links(), HasLen, 0)
	}

	// A check that's already running isn't started again, and nothing is
	// checkpointed.
	c.Assert(e.planMgr.AppendLayer(context.Background(), checksLayer("layer2", plan.CheckStartupEnabled, "chk3"), false), IsNil)
	c.Assert(e.checkInfo(c, "chk3").ChangeID, Not(Equals), "")
	s.recorder.Reset()
	started, err = e.mgr.StartChecks(requestCtx, []string{"chk3"})
	c.Assert(err, IsNil)
	c.Check(started, HasLen, 0)
	c.Check(s.recorder.Ended(), HasLen, 0)
}

func (s *tracingSuite) TestStopChecksLinksChangeSpan(c *C) {
	e := s.newCheckManager(c)
	c.Assert(e.planMgr.AppendLayer(context.Background(), checksLayer("layer1", plan.CheckStartupEnabled, "chk1"), false), IsNil)
	changeID := e.checkInfo(c, "chk1").ChangeID
	c.Assert(changeID, Not(Equals), "")
	s.recorder.Reset()

	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()
	stopped, err := e.mgr.StopChecks(requestCtx, []string{"chk1"})
	c.Assert(err, IsNil)
	c.Assert(stopped, DeepEquals, []string{"chk1"})

	// The change's span (ended by aborting it before it ran) is linked to
	// the request, but remains in the trace of whatever started it.
	span := s.changeSpan(c, changeID)
	c.Check(span.Name(), Equals, "change perform-check")
	c.Check(span.SpanContext().TraceID(), Not(Equals), requestSpan.SpanContext().TraceID())
	c.Assert(span.Links(), HasLen, 1)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.Links()[0].SpanContext.TraceID(), Equals, requestSpan.SpanContext().TraceID())
	c.Check(spanAttrs(span)["pebble.change.status"].AsString(), Equals, "Hold")

	// Stopping the check checkpointed the state on behalf of the request.
	checkpoints := s.checkpointsCausedBy(requestSpan.SpanContext())
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())

	// Stopping a check that isn't running does nothing.
	s.recorder.Reset()
	stopped, err = e.mgr.StopChecks(requestCtx, []string{"chk1"})
	c.Assert(err, IsNil)
	c.Check(stopped, HasLen, 0)
	c.Check(s.recorder.Ended(), HasLen, 0)
}

func (s *tracingSuite) TestReplanChangeSpans(c *C) {
	e := s.newCheckManager(c)
	c.Assert(e.planMgr.AppendLayer(context.Background(), checksLayer("layer1", plan.CheckStartupEnabled, "chk1"), false), IsNil)
	c.Assert(e.planMgr.AppendLayer(context.Background(), checksLayer("layer2", plan.CheckStartupDisabled, "chk2"), false), IsNil)
	_, err := e.mgr.StopChecks(context.Background(), []string{"chk1"})
	c.Assert(err, IsNil)
	c.Assert(e.checkInfo(c, "chk1").ChangeID, Equals, "")
	s.recorder.Reset()

	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()
	e.st.Lock()
	e.mgr.Replan(requestCtx)
	e.st.Unlock()

	// The stopped "startup: enabled" check is started again, as part of the
	// request. The "startup: disabled" check isn't.
	changeID := e.checkInfo(c, "chk1").ChangeID
	c.Assert(changeID, Not(Equals), "")
	c.Check(e.checkInfo(c, "chk2").ChangeID, Equals, "")

	checkpoints := s.checkpointsCausedBy(requestSpan.SpanContext())
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())

	e.st.Lock()
	e.st.Change(changeID).Abort()
	e.st.Unlock()
	span := s.changeSpan(c, changeID)
	c.Check(span.Name(), Equals, "change perform-check")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
}

func (s *tracingSuite) TestPlanChangedChangeSpans(c *C) {
	e := s.newCheckManager(c)

	// The layer added via the plan manager starts its checks as part of the
	// plan change.
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()
	c.Assert(e.planMgr.AppendLayer(requestCtx, checksLayer("layer1", plan.CheckStartupEnabled, "chk1"), false), IsNil)
	layerSpan := s.endedSpan(c, "plan add layer")
	c.Check(layerSpan.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())

	chk1ChangeID := e.checkInfo(c, "chk1").ChangeID
	c.Assert(chk1ChangeID, Not(Equals), "")
	checkpoints := s.checkpointsCausedBy(layerSpan.SpanContext())
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())

	// A plan change that modifies the check stops its change, and starts a
	// new one as part of the plan change.
	s.recorder.Reset()
	modified := *e.planMgr.Plan().Checks["chk1"]
	modified.Threshold = 5
	newPlan := &plan.Plan{Checks: map[string]*plan.Check{"chk1": &modified}}
	planCtx, planSpan := s.startSpan("plan change")
	defer planSpan.End()
	e.mgr.PlanChanged(planCtx, newPlan)

	chk1NewChangeID := e.checkInfo(c, "chk1").ChangeID
	c.Assert(chk1NewChangeID, Not(Equals), "")
	c.Assert(chk1NewChangeID, Not(Equals), chk1ChangeID)
	checkpoints = s.checkpointsCausedBy(planSpan.SpanContext())
	c.Assert(checkpoints, HasLen, 1)

	e.st.Lock()
	e.st.Change(chk1NewChangeID).Abort()
	e.st.Unlock()
	for _, changeID := range []string{chk1ChangeID, chk1NewChangeID} {
		span := s.changeSpan(c, changeID)
		c.Check(span.Name(), Equals, "change perform-check")
		c.Check(span.Links(), HasLen, 0)
		switch changeID {
		case chk1ChangeID:
			c.Check(span.Parent().SpanID(), Equals, layerSpan.SpanContext().SpanID())
			c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
		case chk1NewChangeID:
			c.Check(span.Parent().SpanID(), Equals, planSpan.SpanContext().SpanID())
			c.Check(span.SpanContext().TraceID(), Equals, planSpan.SpanContext().TraceID())
		}
	}
}

func (s *tracingSuite) TestPerformRecoverChangesLinked(c *C) {
	c.Assert(reaper.Start(), IsNil)
	defer reaper.Stop()

	e := s.newCheckManager(c)
	var failureCtxs []context.Context
	e.mgr.NotifyCheckFailed(func(ctx context.Context, name string) {
		failureCtxs = append(failureCtxs, ctx)
	})

	// The check fails while the file exists.
	testPath := c.MkDir() + "/test"
	c.Assert(os.WriteFile(testPath, nil, 0o644), IsNil)
	const threshold = 2
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()
	e.mgr.PlanChanged(requestCtx, &plan.Plan{Checks: map[string]*plan.Check{
		"chk1": {
			Name:      "chk1",
			Override:  plan.ReplaceOverride,
			Period:    plan.OptionalDuration{Value: 10 * time.Millisecond},
			Timeout:   plan.OptionalDuration{Value: time.Second},
			Threshold: threshold,
			Exec:      &plan.ExecCheck{Command: fmt.Sprintf("test ! -f %s", testPath)},
		},
	}})
	performID := e.checkInfo(c, "chk1").ChangeID
	c.Assert(performID, Not(Equals), "")

	// Reaching the failure threshold fails the perform-check change and
	// starts a recover-check change.
	var recoverID string
	e.ensureUntil(c, "the check to reach its threshold", func() bool {
		info := e.checkInfo(c, "chk1")
		if info.Status != CheckStatusDown || info.ChangeID == performID {
			return false
		}
		recoverID = info.ChangeID
		c.Check(info.PrevChangeID, Equals, performID)
		return true
	})
	// The failure handler was told the failing check run.
	c.Assert(failureCtxs, HasLen, 1)
	failureSC := tracing.SpanContextFromContext(failureCtxs[0])
	var checkRunSpan tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == "check chk1" && span.SpanContext().SpanID() == failureSC.SpanID() {
			checkRunSpan = span
		}
	}
	c.Assert(checkRunSpan, NotNil)
	c.Check(checkRunSpan.Status().Code, Equals, tracing.StatusError)

	// The check succeeding again finishes the recover-check change and
	// starts a new perform-check change.
	c.Assert(os.Remove(testPath), IsNil)
	var performAgainID string
	e.ensureUntil(c, "the check to recover", func() bool {
		info := e.checkInfo(c, "chk1")
		if info.Status != CheckStatusUp || info.ChangeID == recoverID {
			return false
		}
		performAgainID = info.ChangeID
		c.Check(info.PrevChangeID, Equals, recoverID)
		return true
	})

	// Stopping the runner stops the check task, finishing the change that's
	// in progress and so ending its span.
	e.runner.Stop()

	// The first perform-check change is part of the plan change's trace.
	performSpan := s.changeSpan(c, performID)
	c.Check(performSpan.Name(), Equals, "change perform-check")
	c.Check(performSpan.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(performSpan.Status().Code, Equals, tracing.StatusError)
	c.Check(performSpan.Links(), HasLen, 0)

	// The recover-check change is a new trace, linked to the failed
	// perform-check change.
	recoverSpan := s.changeSpan(c, recoverID)
	c.Check(recoverSpan.Name(), Equals, "change recover-check")
	c.Check(recoverSpan.Parent().IsValid(), Equals, false)
	c.Check(recoverSpan.SpanContext().TraceID(), Not(Equals), performSpan.SpanContext().TraceID())
	c.Check(spanAttrs(recoverSpan)["pebble.change.status"].AsString(), Equals, "Done")
	c.Assert(recoverSpan.Links(), HasLen, 1)
	c.Check(recoverSpan.Links()[0].SpanContext.TraceID(), Equals, performSpan.SpanContext().TraceID())
	c.Check(recoverSpan.Links()[0].SpanContext.SpanID(), Equals, performSpan.SpanContext().SpanID())

	// The next perform-check change is again a new trace, linked to the
	// recover-check change.
	performAgainSpan := s.changeSpan(c, performAgainID)
	c.Check(performAgainSpan.Name(), Equals, "change perform-check")
	c.Check(performAgainSpan.Parent().IsValid(), Equals, false)
	c.Check(performAgainSpan.SpanContext().TraceID(), Not(Equals), recoverSpan.SpanContext().TraceID())
	c.Check(spanAttrs(performAgainSpan)["pebble.change.status"].AsString(), Equals, "Done")
	c.Assert(performAgainSpan.Links(), HasLen, 1)
	c.Check(performAgainSpan.Links()[0].SpanContext.TraceID(), Equals, recoverSpan.SpanContext().TraceID())
	c.Check(performAgainSpan.Links()[0].SpanContext.SpanID(), Equals, recoverSpan.SpanContext().SpanID())
}
