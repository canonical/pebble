// Copyright (c) 2014-2020 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/state"
)

type stateEngineSuite struct{}

var _ = Suite(&stateEngineSuite{})

func (ses *stateEngineSuite) TestNewAndState(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	c.Check(se.State(), Equals, s)
}

type fakeManager struct {
	name                   string
	calls                  chan<- string
	ensureError, stopError error
}

func (fm *fakeManager) Ensure() error {
	fm.calls <- "ensure:" + fm.name
	return fm.ensureError
}

func (fm *fakeManager) Stop() {
	fm.calls <- "stop:" + fm.name
}

func (fm *fakeManager) Wait() {
	fm.calls <- "wait:" + fm.name
}

var _ overlord.StateManager = (*fakeManager)(nil)

func (ses *stateEngineSuite) TestEnsure(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := make(chan string, 4)

	mgr1 := &fakeManager{name: "mgr1", calls: calls}
	mgr2 := &fakeManager{name: "mgr2", calls: calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Assert(err, IsNil)
	checkCalls(c, calls, "ensure:mgr1", "ensure:mgr2")

	err = se.Ensure()
	c.Assert(err, IsNil)
	checkCalls(c, calls, "ensure:mgr1", "ensure:mgr2")
}

func (ses *stateEngineSuite) TestEnsureError(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := make(chan string, 2)

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: calls, ensureError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: calls, ensureError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Check(err.Error(), DeepEquals, "state ensure errors: [boom1 boom2]")
	checkCalls(c, calls, "ensure:mgr1", "ensure:mgr2")
}

func (ses *stateEngineSuite) TestStop(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := make(chan string, 2)

	mgr1 := &fakeManager{name: "mgr1", calls: calls}
	mgr2 := &fakeManager{name: "mgr2", calls: calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	se.Stop()
	checkCalls(c, calls, "stop:mgr1", "stop:mgr2")
	se.Stop()
	c.Check(len(calls), Equals, 0)

	err := se.Ensure()
	c.Check(err, ErrorMatches, "state engine already stopped")
}

func checkCalls(c *C, calls <-chan string, expected ...string) {
	// Initialise multiset containing calls
	expectedCalls := map[string]int{}
	for _, expCall := range expected {
		expectedCalls[expCall]++
	}

loop:
	for {
		select {
		case call := <-calls:
			expectedCalls[call]--
			if expectedCalls[call] < 0 {
				c.Errorf("extra call: %q", call)
			}
		default:
			break loop
		}
	}

	for call, n := range expectedCalls {
		if n > 0 {
			c.Errorf("missing %d calls: %q", n, call)
		}
	}
}
