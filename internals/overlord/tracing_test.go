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
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord"
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
