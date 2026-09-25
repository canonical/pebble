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

package state_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

type tracingSuite struct {
	recorder *tracingtest.Recorder
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.recorder = tracingtest.NewRecorder()
}

func (s *tracingSuite) TearDownTest(c *C) {
	s.recorder.Restore()
}

func (s *tracingSuite) spans() map[string]tracingtest.ReadOnlySpan {
	spans := make(map[string]tracingtest.ReadOnlySpan)
	for _, span := range s.recorder.Ended() {
		spans[span.Name()] = span
	}
	return spans
}

func spanAttr(span tracingtest.ReadOnlySpan, key string) tracing.AttributeValue {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == cmd.ProgramName+"."+key {
			return kv.Value
		}
	}
	return tracing.AttributeValue{}
}

func (s *tracingSuite) TestChangeAndTaskSpans(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	var handlerSpan tracing.SpanContext
	r.AddHandler("foo", func(t *state.Task, tb *tomb.Tomb) error {
		//lint:ignore SA1012 providing a nil context to tomb.Context() is valid
		handlerSpan = tracing.SpanContextFromContext(tb.Context(nil))
		return nil
	}, nil)
	r.AddHandler("bar", func(t *state.Task, tb *tomb.Tomb) error {
		return errors.New("boom")
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "Install things")
	foo := st.NewTask("foo", "Foo task")
	bar := st.NewTask("bar", "Bar task")
	bar.WaitFor(foo)
	chg.AddTask(foo)
	chg.AddTask(bar)
	st.Unlock()

	ensureChange(c, r, sb, chg)

	st.Lock()
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	st.Unlock()

	spans := s.spans()
	chgSpan, ok := spans["change install"]
	c.Assert(ok, Equals, true)
	c.Check(chgSpan.Status().Code, Equals, tracing.StatusError)
	c.Check(spanAttr(chgSpan, "change.id").AsString(), Equals, chg.ID())
	c.Check(spanAttr(chgSpan, "change.status").AsString(), Equals, "Error")

	fooSpan, ok := spans["do foo"]
	c.Assert(ok, Equals, true)
	c.Check(fooSpan.Parent().SpanID(), Equals, chgSpan.SpanContext().SpanID())
	c.Check(fooSpan.SpanContext().TraceID(), Equals, chgSpan.SpanContext().TraceID())
	c.Check(fooSpan.Status().Code, Equals, tracing.StatusUnset)
	c.Check(spanAttr(fooSpan, "task.status").AsString(), Equals, "Done")
	c.Check(handlerSpan.SpanID(), Equals, fooSpan.SpanContext().SpanID())

	barSpan, ok := spans["do bar"]
	c.Assert(ok, Equals, true)
	c.Check(barSpan.Parent().SpanID(), Equals, chgSpan.SpanContext().SpanID())
	c.Check(barSpan.Status().Code, Equals, tracing.StatusError)
	c.Check(barSpan.Status().Description, Equals, "boom")
	c.Check(spanAttr(barSpan, "task.status").AsString(), Equals, "Error")
}

func (s *tracingSuite) TestChangeSpanResumedAfterRestart(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "Install things")
	chg.AddTask(st.NewTask("foo", "Foo task"))
	origSpan := chg.SpanContext()
	data, err := json.Marshal(st)
	c.Assert(err, IsNil)
	st.Unlock()
	c.Assert(origSpan.IsValid(), Equals, true)

	st2, err := state.ReadState(nil, bytes.NewReader(data))
	c.Assert(err, IsNil)
	st2.Lock()
	chg2 := st2.Change(chg.ID())
	c.Check(chg2.SpanContext().TraceID(), Equals, origSpan.TraceID())
	c.Check(chg2.SpanContext().SpanID(), Equals, origSpan.SpanID())
	for _, t := range chg2.Tasks() {
		t.SetStatus(state.DoneStatus)
	}
	st2.Unlock()

	var resumed tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if spanAttr(span, "change.resumed").AsBool() {
			resumed = span
		}
	}
	c.Assert(resumed, NotNil)
	c.Check(resumed.SpanContext().TraceID(), Equals, origSpan.TraceID())
	c.Check(resumed.Parent().SpanID(), Equals, origSpan.SpanID())
}

func (s *tracingSuite) TestNewChangeContext(c *C) {
	parent := tracing.NewSpanContext(tracing.SpanContextConfig{
		TraceID:    tracing.TraceID{1},
		SpanID:     tracing.SpanID{2},
		TraceFlags: tracing.FlagsSampled,
		Remote:     true,
	})
	ctx := tracing.ContextWithRemoteSpanContext(context.Background(), parent)

	st := state.New(nil)
	st.Lock()
	chg := st.NewChangeContext(ctx, "exec", "Execute command")
	chg.SetStatus(state.DoneStatus)
	st.Unlock()

	spans := s.spans()
	chgSpan, ok := spans["change exec"]
	c.Assert(ok, Equals, true)
	c.Check(chgSpan.SpanContext().TraceID(), Equals, parent.TraceID())
	c.Check(chgSpan.Parent().SpanID(), Equals, parent.SpanID())
	c.Check(chgSpan.Parent().IsRemote(), Equals, true)
}

func (s *tracingSuite) TestCheckpointSpan(c *C) {
	restore := state.FakeCheckpointRetryDelay(time.Millisecond, time.Second)
	defer restore()

	failures := 1
	b := &fakeStateBackend{error: func() error {
		if failures > 0 {
			failures--
			return errors.New("disk full")
		}
		return nil
	}}
	st := state.New(b)
	st.Lock()
	st.Set("k", "v")
	st.Unlock()

	span, ok := s.spans()["state checkpoint"]
	c.Assert(ok, Equals, true)
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Check(spanAttr(span, "state.size").AsInt64(), Equals, int64(len(b.checkpoints[1])))
	// The failed attempt is recorded, but the checkpoint succeeded.
	c.Assert(span.Events(), HasLen, 1)
	c.Check(span.Events()[0].Name, Equals, "exception")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)

	// Unlocking without changes doesn't checkpoint.
	s.recorder.Reset()
	st.Lock()
	st.Unlock()
	c.Check(s.recorder.Ended(), HasLen, 0)
}

// checkpoints returns the ended "state checkpoint" spans.
func (s *tracingSuite) checkpoints() []tracingtest.ReadOnlySpan {
	var spans []tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == "state checkpoint" {
			spans = append(spans, span)
		}
	}
	return spans
}

func (s *tracingSuite) TestCheckpointCausedByTaskHandler(c *C) {
	st := state.New(&fakeStateBackend{})
	r := state.NewTaskRunner(st)
	defer r.Stop()

	handlerDone := make(chan struct{})
	r.AddHandler("foo", func(t *state.Task, tb *tomb.Tomb) error {
		defer close(handlerDone)
		// Wait for the runner to checkpoint starting the task.
		st.Lock()
		st.Unlock()
		s.recorder.Reset()

		st.Lock()
		t.Set("progress", 1)
		st.Unlock()
		return nil
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "Install things")
	chg.AddTask(st.NewTask("foo", "Foo task"))
	st.Unlock()

	c.Assert(r.Ensure(), IsNil)
	<-handlerDone
	r.Wait()

	// The checkpoint made by the handler is part of the task's tracing.
	var taskSpan tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == "do foo" {
			taskSpan = span
		}
	}
	c.Assert(taskSpan, NotNil)
	checkpoints := s.checkpoints()
	c.Assert(len(checkpoints) >= 1, Equals, true)
	c.Check(checkpoints[0].Parent().SpanID(), Equals, taskSpan.SpanContext().SpanID())
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, taskSpan.SpanContext().TraceID())
	c.Check(checkpoints[0].Links(), HasLen, 0)
}

func (s *tracingSuite) TestCheckpointCausedByChange(c *C) {
	st := state.New(&fakeStateBackend{})

	st.Lock()
	chg := st.NewChange("install", "Install things")
	st.Unlock()
	s.recorder.Reset()

	st.Lock()
	chg.Set("foo", "bar")
	chgSpan := chg.SpanContext()
	st.Unlock()

	checkpoints := s.checkpoints()
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].Parent().SpanID(), Equals, chgSpan.SpanID())
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, chgSpan.TraceID())
}

func (s *tracingSuite) TestCheckpointExplicitCauseTakesPrecedence(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	chg1 := st.NewChange("one", "One")
	chg2 := st.NewChange("two", "Two")
	st.Unlock()
	s.recorder.Reset()

	requestSC := tracing.NewSpanContext(tracing.SpanContextConfig{
		TraceID:    tracing.TraceID{1},
		SpanID:     tracing.SpanID{2},
		TraceFlags: tracing.FlagsSampled,
		Remote:     true,
	})
	requestCtx := tracing.ContextWithRemoteSpanContext(context.Background(), requestSC)

	st.Lock()
	chg1.Set("a", 1)
	st.AddTraceContext(requestCtx)
	chg2.Set("b", 2)
	chg1.Set("c", 3) // Already a cause, so not duplicated.
	st.Unlock()

	checkpoints := s.checkpoints()
	c.Assert(checkpoints, HasLen, 1)
	checkpoint := checkpoints[0]
	c.Check(checkpoint.Parent().SpanID(), Equals, requestSC.SpanID())
	c.Check(checkpoint.SpanContext().TraceID(), Equals, requestSC.TraceID())
	c.Assert(checkpoint.Links(), HasLen, 2)
	c.Check(checkpoint.Links()[0].SpanContext.SpanID(), Equals, chg1.SpanContext().SpanID())
	c.Check(checkpoint.Links()[1].SpanContext.SpanID(), Equals, chg2.SpanContext().SpanID())

	// Causes don't carry over to later modifications.
	s.recorder.Reset()
	st.Lock()
	st.Set("k", "v")
	st.Unlock()
	checkpoints = s.checkpoints()
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].Parent().IsValid(), Equals, false)
	c.Check(checkpoints[0].Links(), HasLen, 0)
}

func (s *tracingSuite) TestPruneSpan(c *C) {
	st := state.New(&fakeStateBackend{})
	now := time.Now()
	pruneWait := time.Hour
	abortWait := 3 * time.Hour

	st.Lock()
	// An unready change past abortWait, which is aborted.
	chg1 := st.NewChange("abort", "...")
	chg1.AddTask(st.NewTask("foo", "..."))
	state.FakeChangeTimes(chg1, now.Add(-abortWait), time.Time{})
	// A change that became ready more than pruneWait ago, which is removed
	// along with its task.
	chg2 := st.NewChange("prune", "...")
	chg2.AddTask(st.NewTask("foo", "..."))
	state.FakeChangeTimes(chg2, now.Add(-pruneWait), now.Add(-pruneWait))
	// An old unlinked task, which is removed.
	t := st.NewTask("unlinked", "...")
	state.FakeTaskTimes(t, now.Add(-pruneWait), now.Add(-pruneWait))
	st.Unlock()
	s.recorder.Reset()

	st.Lock()
	st.Prune(now.AddDate(-1, 0, 0), pruneWait, abortWait, 100, 100)
	st.Unlock()

	spans := s.spans()
	pruneSpan, ok := spans["state prune"]
	c.Assert(ok, Equals, true)
	c.Check(pruneSpan.Parent().IsValid(), Equals, false)
	c.Check(spanAttr(pruneSpan, "state.prune.changes").AsInt64(), Equals, int64(1))
	c.Check(spanAttr(pruneSpan, "state.prune.tasks").AsInt64(), Equals, int64(2))
	// The pruned change's change-update notice is removed with it.
	c.Check(spanAttr(pruneSpan, "state.prune.notices").AsInt64(), Equals, int64(1))
	c.Check(spanAttr(pruneSpan, "state.prune.aborted-changes").AsInt64(), Equals, int64(1))

	// The resulting checkpoint is part of the prune's trace, linked to the
	// aborted change.
	checkpoints := s.checkpoints()
	c.Assert(checkpoints, HasLen, 1)
	c.Check(checkpoints[0].Parent().SpanID(), Equals, pruneSpan.SpanContext().SpanID())
	c.Check(checkpoints[0].SpanContext().TraceID(), Equals, pruneSpan.SpanContext().TraceID())
	c.Assert(checkpoints[0].Links(), HasLen, 1)
	c.Check(checkpoints[0].Links()[0].SpanContext.SpanID(), Equals, chg1.SpanContext().SpanID())
}

func (s *tracingSuite) TestCleanupNotInChangeTrace(c *C) {
	st := state.New(&fakeStateBackend{})
	r := state.NewTaskRunner(st)
	defer r.Stop()

	r.AddHandler("foo", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)
	r.AddHandler("bar", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)
	r.AddCleanup("bar", func(t *state.Task, tb *tomb.Tomb) error { return nil })

	st.Lock()
	chg := st.NewChange("install", "Install things")
	chg.AddTask(st.NewTask("foo", "Foo task"))
	chg.AddTask(st.NewTask("bar", "Bar task"))
	st.Unlock()

	// Run the tasks to completion. Nothing schedules another ensure, so
	// they're cleaned on a later ensure pass.
	c.Assert(r.Ensure(), IsNil)
	r.Wait()
	st.Lock()
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.IsClean(), Equals, false)
	chgSpan := chg.SpanContext()
	st.Unlock()
	s.recorder.Reset()

	c.Assert(r.Ensure(), IsNil)
	r.Wait()
	st.Lock()
	c.Assert(chg.IsClean(), Equals, true)
	st.Unlock()

	spans := s.spans()
	for name, span := range spans {
		c.Check(span.SpanContext().TraceID(), Not(Equals), chgSpan.TraceID(), Commentf("span %q in the change's trace", name))
	}

	// The task without a cleanup handler is marked clean by the ensure
	// pass, in its own trace linked to the change.
	cleanSpan, ok := spans["clean tasks"]
	c.Assert(ok, Equals, true)
	c.Check(cleanSpan.Parent().IsValid(), Equals, false)
	c.Check(spanAttr(cleanSpan, "task.cleaned").AsInt64(), Equals, int64(1))
	c.Assert(cleanSpan.Links(), HasLen, 1)
	c.Check(cleanSpan.Links()[0].SpanContext.SpanID(), Equals, chgSpan.SpanID())

	// The cleanup handler runs in its own trace, linked to the change.
	cleanupSpan, ok := spans["cleanup bar"]
	c.Assert(ok, Equals, true)
	c.Check(cleanupSpan.Parent().IsValid(), Equals, false)
	c.Assert(cleanupSpan.Links(), HasLen, 1)
	c.Check(cleanupSpan.Links()[0].SpanContext.SpanID(), Equals, chgSpan.SpanID())

	// Each resulting checkpoint is part of the trace that caused it.
	checkpoints := s.checkpoints()
	c.Assert(checkpoints, HasLen, 2)
	parents := map[tracing.SpanID]bool{}
	for _, checkpoint := range checkpoints {
		parents[checkpoint.Parent().SpanID()] = true
	}
	c.Check(parents, DeepEquals, map[tracing.SpanID]bool{
		cleanSpan.SpanContext().SpanID():   true,
		cleanupSpan.SpanContext().SpanID(): true,
	})
}

func (s *tracingSuite) TestTraceCausesReset(c *C) {
	st := state.New(&fakeStateBackend{})
	requestCtx := tracing.ContextWithRemoteSpanContext(context.Background(), tracing.NewSpanContext(tracing.SpanContextConfig{
		TraceID:    tracing.TraceID{1},
		SpanID:     tracing.SpanID{2},
		TraceFlags: tracing.FlagsSampled,
	}))

	// Small slices are emptied, keeping their backing arrays.
	st.Lock()
	st.AddTraceContext(requestCtx)
	st.NewChange("one", "One")
	st.NewChange("two", "Two")
	explicitLen, _, implicitLen, _ := st.TraceCauses()
	c.Assert(explicitLen, Equals, 1)
	c.Assert(implicitLen, Equals, 2)
	st.Unlock()

	st.Lock()
	explicitLen, explicitCap, implicitLen, implicitCap := st.TraceCauses()
	st.Unlock()
	c.Check(explicitLen, Equals, 0)
	c.Check(explicitCap >= 1, Equals, true)
	c.Check(implicitLen, Equals, 0)
	c.Check(implicitCap >= 2, Equals, true)

	// Slices that grow beyond 128 are dropped.
	st.Lock()
	for range 129 {
		st.NewChange("many", "Many")
	}
	_, _, _, implicitCap = st.TraceCauses()
	c.Assert(implicitCap > 128, Equals, true)
	st.Unlock()

	st.Lock()
	explicitLen, explicitCap, implicitLen, implicitCap = st.TraceCauses()
	st.Unlock()
	c.Check(explicitLen, Equals, 0)
	c.Check(explicitCap >= 1, Equals, true) // Still small, so kept.
	c.Check(implicitLen, Equals, 0)
	c.Check(implicitCap, Equals, 0)
}
