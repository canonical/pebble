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

package overlord_test

import (
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

// checkStartupCheckpoints checks that every state checkpoint made while
// creating and starting up an overlord is part of the "state load" or
// "overlord startup" trace, and returns the number of checkpoints.
func (ovs *overlordSuite) checkStartupCheckpoints(c *C, opts *overlord.Options) int {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	o, err := overlord.New(opts)
	c.Assert(err, IsNil)
	c.Assert(o.StartUp(), IsNil)
	o.Stop()

	startupSpans := make(map[tracing.SpanID]tracingtest.ReadOnlySpan)
	var checkpoints []tracingtest.ReadOnlySpan
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case "state load", "overlord startup":
			startupSpans[span.SpanContext().SpanID()] = span
		case "state checkpoint":
			checkpoints = append(checkpoints, span)
		}
	}
	c.Assert(startupSpans, HasLen, 2)
	for _, checkpoint := range checkpoints {
		parent, ok := startupSpans[checkpoint.Parent().SpanID()]
		if c.Check(ok, Equals, true, Commentf("checkpoint without a startup parent")) {
			c.Check(checkpoint.SpanContext().TraceID(), Equals, parent.SpanContext().TraceID())
		}
	}
	return len(checkpoints)
}

func (ovs *overlordSuite) TestStartupCheckpointsTraced(c *C) {
	// A fresh state is initialised and patched.
	n := ovs.checkStartupCheckpoints(c, &overlord.Options{PebbleDir: ovs.dir})
	c.Check(n > 0, Equals, true)
	// An existing state is loaded.
	ovs.checkStartupCheckpoints(c, &overlord.Options{PebbleDir: ovs.dir})
	// A state that isn't persisted is set up.
	ovs.checkStartupCheckpoints(c, &overlord.Options{PebbleDir: c.MkDir(), Persist: overlord.PersistNever})
}

func spanAttr(span tracingtest.ReadOnlySpan, key string) tracing.AttributeValue {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == cmd.ProgramName+"."+key {
			return kv.Value
		}
	}
	return tracing.AttributeValue{}
}

func (ovs *overlordSuite) TestStopEndsInFlightChangeSpans(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	o, err := overlord.New(&overlord.Options{PebbleDir: ovs.dir})
	c.Assert(err, IsNil)
	defer o.Stop()

	// A task that only returns once the overlord is stopping, so that its
	// change is still in progress then.
	started := make(chan struct{})
	o.TaskRunner().AddHandler("block", func(t *state.Task, tb *tomb.Tomb) error {
		close(started)
		<-tb.Dying()
		return &state.Retry{}
	}, nil)
	c.Assert(o.StartUp(), IsNil)

	st := o.State()
	st.Lock()
	blocked := st.NewChange("blocked", "...")
	blocked.AddTask(st.NewTask("block", "..."))
	finished := st.NewChange("finished", "...")
	finished.SetStatus(state.DoneStatus)
	st.Unlock()

	o.Loop()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		c.Fatal("timed out waiting for the task to start")
	}
	c.Assert(o.Stop(), IsNil)

	st.Lock()
	c.Check(blocked.Status(), Equals, state.DoingStatus)
	st.Unlock()

	spans := make(map[string]tracingtest.ReadOnlySpan)
	for _, span := range recorder.Ended() {
		spans[span.Name()] = span
	}

	// The in-flight change's span is ended by stopping, as interrupted.
	blockedSpan, ok := spans["change blocked"]
	c.Assert(ok, Equals, true)
	c.Check(spanAttr(blockedSpan, "change.id").AsString(), Equals, blocked.ID())
	c.Check(spanAttr(blockedSpan, "change.interrupted").AsBool(), Equals, true)
	c.Check(spanAttr(blockedSpan, "change.status").AsString(), Equals, "Doing")
	c.Check(blockedSpan.Status().Code, Equals, tracing.StatusUnset)
	// Its task's span ended when the task runner stopped it, before the change's.
	taskSpan, ok := spans["do block"]
	c.Assert(ok, Equals, true)
	c.Check(taskSpan.Parent().SpanID(), Equals, blockedSpan.SpanContext().SpanID())
	c.Check(taskSpan.EndTime().After(blockedSpan.EndTime()), Equals, false)

	// The change that was already ready ended normally.
	finishedSpan, ok := spans["change finished"]
	c.Assert(ok, Equals, true)
	c.Check(spanAttr(finishedSpan, "change.interrupted").AsBool(), Equals, false)
	c.Check(spanAttr(finishedSpan, "change.status").AsString(), Equals, "Done")

	// Stopping again (as happens with the deferred Stop) ends nothing twice.
	recorder.Reset()
	c.Assert(o.Stop(), IsNil)
	c.Check(recorder.Ended(), HasLen, 0)
}
