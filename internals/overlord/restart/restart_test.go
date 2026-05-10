// Copyright (c) 2014-2021 Canonical Ltd
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

package restart_test

import (
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
)

type restartSuite struct{}

func TestRestartSuite(t *testing.T) {
	tc.Run(t, &restartSuite{})
}

type testHandler struct {
	restartRequested   bool
	rebootAsExpected   bool
	rebootDidNotHappen bool
}

func (h *testHandler) HandleRestart(t restart.RestartType) {
	h.restartRequested = true
}

func (h *testHandler) RebootAsExpected(*state.State) error {
	h.rebootAsExpected = true
	return nil
}

func (h *testHandler) RebootDidNotHappen(*state.State) error {
	h.rebootDidNotHappen = true
	return nil
}

func (s *restartSuite) TestManager(c *tc.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	mgr, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, tc.IsNil)
	c.Check(mgr, tc.FitsTypeOf, &restart.RestartManager{})
}

func (s *restartSuite) TestRequestRestartDaemon(c *tc.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	h := &testHandler{}

	manager, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, tc.IsNil)
	c.Check(h.rebootAsExpected, tc.Equals, true)

	ok, t := manager.Pending()
	c.Check(ok, tc.Equals, false)
	c.Check(t, tc.Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartDaemon)

	c.Check(h.restartRequested, tc.Equals, true)

	ok, t = manager.Pending()
	c.Check(ok, tc.Equals, true)
	c.Check(t, tc.Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartDaemonNoHandler(c *tc.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	manager, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, tc.IsNil)

	restart.Request(st, restart.RestartDaemon)

	ok, t := manager.Pending()
	c.Check(ok, tc.Equals, true)
	c.Check(t, tc.Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartSystemAndVerifyReboot(c *tc.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	manager, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, tc.IsNil)
	c.Check(h.rebootAsExpected, tc.Equals, true)

	ok, t := manager.Pending()
	c.Check(ok, tc.Equals, false)
	c.Check(t, tc.Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartSystem)

	c.Check(h.restartRequested, tc.Equals, true)

	ok, t = manager.Pending()
	c.Check(ok, tc.Equals, true)
	c.Check(t, tc.Equals, restart.RestartSystem)

	var fromBootID string
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), tc.IsNil)
	c.Check(fromBootID, tc.Equals, "boot-id-1")

	h1 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-1", h1)
	c.Assert(err, tc.IsNil)
	c.Check(h1.rebootAsExpected, tc.Equals, false)
	c.Check(h1.rebootDidNotHappen, tc.Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), tc.IsNil)
	c.Check(fromBootID, tc.Equals, "boot-id-1")

	h2 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-2", h2)
	c.Assert(err, tc.IsNil)
	c.Check(h2.rebootAsExpected, tc.Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), tc.ErrorIs, state.ErrNoState)
}
