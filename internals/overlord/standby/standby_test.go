// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (tc.C) 2018 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
package standby_test

import (
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate/servstatetest"
	"github.com/canonical/pebble/internals/overlord/standby"
	"github.com/canonical/pebble/internals/overlord/state"
)

type standbySuite struct {
	state *state.State

	canStandby bool
}

func TestStandbySuite(t *testing.T) {
	tc.Run(t, &standbySuite{})
}

func (s *standbySuite) SetUpTest(c *tc.C) {
	s.state = state.New(nil)

	c.Cleanup(func() {
		s.state = nil
		s.canStandby = false
	})
}

func (s *standbySuite) TestCanStandbyNoChanges(c *tc.C) {
	m := standby.New(s.state)
	c.Check(m.CanStandby(), tc.Equals, false)

	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), tc.Equals, true)
}

func (s *standbySuite) TestCanStandbyPendingChanges(c *tc.C) {
	st := s.state
	st.Lock()
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(st.NewTask("bar", "fake task"))
	c.Assert(chg.Status(), tc.Equals, state.DoStatus)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), tc.Equals, false)
}

func (s *standbySuite) TestCanStandbyPendingClean(c *tc.C) {
	st := s.state
	st.Lock()
	t := st.NewTask("bar", "fake task")
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), tc.Equals, state.DoneStatus)
	c.Assert(t.IsClean(), tc.Equals, false)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), tc.Equals, false)
}

func (s *standbySuite) TestCanStandbyOnlyDonePendingChanges(c *tc.C) {
	st := s.state
	st.Lock()
	t := st.NewTask("bar", "fake task")
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	t.SetClean()
	c.Assert(chg.Status(), tc.Equals, state.DoneStatus)
	c.Assert(t.IsClean(), tc.Equals, true)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), tc.Equals, true)
}

func (s *standbySuite) CanStandby() bool {
	return s.canStandby
}

func (s *standbySuite) TestCanStandbyWithOpinion(c *tc.C) {
	m := standby.New(s.state)
	m.AddOpinion(s)
	m.SetStartTime(time.Time{})

	s.canStandby = true
	c.Check(m.CanStandby(), tc.Equals, true)

	s.canStandby = false
	c.Check(m.CanStandby(), tc.Equals, false)
}

type opine func() bool

func (f opine) CanStandby() bool {
	return f()
}

func (s *standbySuite) TestStartChecks(c *tc.C) {
	n := 0
	ch1 := make(chan bool, 1)
	ch2 := make(chan struct{})

	defer standby.FakeStandbyWait(time.Millisecond)()
	s.state.Lock()
	_, err := restart.Manager(s.state, "boot-id-0", servstatetest.FakeRestartHandler(func(t restart.RestartType) {
		c.Check(t, tc.Equals, restart.RestartSocket)
		n++
		<-ch2
	}))
	s.state.Unlock()
	c.Assert(err, tc.ErrorIsNil)

	m := standby.New(s.state)
	m.AddOpinion(opine(func() bool {
		opinion := <-ch1
		return opinion
	}))

	m.Start()
	ch1 <- false
	c.Check(n, tc.Equals, 0)
	ch1 <- false
	c.Check(n, tc.Equals, 0)

	ch1 <- true
	ch2 <- struct{}{}
	c.Check(n, tc.Equals, 1)

	m.Stop()
}

func (s *standbySuite) TestStopWaits(c *tc.C) {
	defer standby.FakeStandbyWait(time.Millisecond)()
	s.state.Lock()
	_, err := restart.Manager(s.state, "boot-id-0", servstatetest.FakeRestartHandler(func(t restart.RestartType) {
		c.Fatal("request restart should have not been called")
	}))
	s.state.Unlock()
	c.Assert(err, tc.ErrorIsNil)

	ch := make(chan struct{})
	opineReady := make(chan struct{})
	done := make(chan struct{})
	m := standby.New(s.state)
	synced := false
	m.AddOpinion(opine(func() bool {
		if !synced {
			// synchronize with the main goroutine only at the
			// beginning
			close(opineReady)
			synced = true
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-done:
		}
		return false
	}))

	m.Start()

	// let the opinionator start its delay
	<-opineReady
	go func() {
		// this will block until standby stops
		m.Stop()
		close(ch)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		// wheee
	case <-ch:
		c.Fatal("stop should have blocked and didn't")
	}

	close(done)

	// wait for Stop to complete now
	select {
	case <-ch:
		// nothing to do here
	case <-time.After(10 * time.Second):
		c.Fatal("stop did not complete")
	}
}
