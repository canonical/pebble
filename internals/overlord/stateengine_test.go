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
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/state"
)

type stateEngineSuite struct{}

func TestStateEngineSuite(t *testing.T) {
	tc.Run(t, &stateEngineSuite{})
}

func (ses *stateEngineSuite) TestNewAndState(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	c.Check(se.State(), tc.Equals, s)
}

type fakeManager struct {
	name                      string
	calls                     *[]string
	ensureError, startupError error
}

func (fm *fakeManager) StartUp() error {
	*fm.calls = append(*fm.calls, "startup:"+fm.name)
	return fm.startupError
}

func (fm *fakeManager) Ensure() error {
	*fm.calls = append(*fm.calls, "ensure:"+fm.name)
	return fm.ensureError
}

func (fm *fakeManager) Stop() {
	*fm.calls = append(*fm.calls, "stop:"+fm.name)
}

func (fm *fakeManager) Wait() {
	*fm.calls = append(*fm.calls, "wait:"+fm.name)
}

var _ overlord.StateManager = (*fakeManager)(nil)

func (ses *stateEngineSuite) TestStartUp(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	mgr1 := &fakeManager{name: "mgr1", calls: &calls}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.StartUp()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(calls, tc.DeepEquals, []string{"startup:mgr1", "startup:mgr2"})

	// noop
	err = se.StartUp()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(calls, tc.HasLen, 2)
}

func (ses *stateEngineSuite) TestStartUpError(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: &calls, startupError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls, startupError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.StartUp()
	c.Check(err.Error(), tc.DeepEquals, "state startup errors: [boom1 boom2]")
	c.Check(calls, tc.DeepEquals, []string{"startup:mgr1", "startup:mgr2"})
}

func (ses *stateEngineSuite) TestEnsure(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	mgr1 := &fakeManager{name: "mgr1", calls: &calls}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Check(err, tc.ErrorMatches, "state engine skipped startup")
	c.Assert(se.StartUp(), tc.IsNil)
	calls = []string{}

	err = se.Ensure()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(calls, tc.DeepEquals, []string{"ensure:mgr1", "ensure:mgr2"})

	err = se.Ensure()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(calls, tc.DeepEquals, []string{"ensure:mgr1", "ensure:mgr2", "ensure:mgr1", "ensure:mgr2"})
}

func (ses *stateEngineSuite) TestEnsureError(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: &calls, ensureError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls, ensureError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	c.Assert(se.StartUp(), tc.IsNil)
	calls = []string{}

	err := se.Ensure()
	c.Check(err.Error(), tc.DeepEquals, "state ensure errors: [boom1 boom2]")
	c.Check(calls, tc.DeepEquals, []string{"ensure:mgr1", "ensure:mgr2"})
}

func (ses *stateEngineSuite) TestStop(c *tc.C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	mgr1 := &fakeManager{name: "mgr1", calls: &calls}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	c.Assert(se.StartUp(), tc.IsNil)
	calls = []string{}

	se.Stop()
	c.Check(calls, tc.DeepEquals, []string{"stop:mgr2", "stop:mgr1"})
	se.Stop()
	c.Check(calls, tc.HasLen, 2)

	err := se.Ensure()
	c.Check(err, tc.ErrorMatches, "state engine already stopped")
}
