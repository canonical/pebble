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

package servstate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
)

// fakeCheckWaiter is a servstate.CheckWaiter that lets tests decide exactly
// when each wait returns, and with what result. Waits deliberately don't
// return when their ctx is canceled, so that tests can deliver a result
// after the service has left the latching state.
type fakeCheckWaiter struct {
	waits  chan *latchWait
	closed chan struct{}
}

// latchWait is a single in-progress call to fakeCheckWaiter.wait.
type latchWait struct {
	check  string
	ctx    context.Context
	result chan error
}

// newFakeCheckWaiter creates a fakeCheckWaiter and sets it as the service
// manager's check waiter. Any waits still in progress at the end of the test
// are released.
func (s *S) newFakeCheckWaiter() *fakeCheckWaiter {
	w := &fakeCheckWaiter{
		waits:  make(chan *latchWait, 10),
		closed: make(chan struct{}),
	}
	s.AddCleanup(func() { close(w.closed) })
	s.manager.SetCheckWaiter(w.wait)
	return w
}

func (w *fakeCheckWaiter) wait(ctx context.Context, check string) error {
	lw := &latchWait{check: check, ctx: ctx, result: make(chan error, 1)}
	w.waits <- lw
	select {
	case err := <-lw.result:
		return err
	case <-w.closed:
		return context.Canceled
	}
}

// next returns the next n waits, sorted by check name.
func (w *fakeCheckWaiter) next(c *C, n int) []*latchWait {
	var waits []*latchWait
	for range n {
		select {
		case lw := <-w.waits:
			waits = append(waits, lw)
		case <-time.After(10 * time.Second):
			c.Fatalf("timed out waiting for latch check waits (got %d of %d)", len(waits), n)
		}
	}
	sort.Slice(waits, func(i, j int) bool { return waits[i].check < waits[j].check })
	return waits
}

// assertNoMoreWaits asserts that no further waits have been started.
func (w *fakeCheckWaiter) assertNoMoreWaits(c *C) {
	select {
	case lw := <-w.waits:
		c.Fatalf("unexpected wait for latch check %q", lw.check)
	default:
	}
}

func waitCanceled(c *C, lw *latchWait) {
	select {
	case <-lw.ctx.Done():
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for latch check %q wait to be canceled", lw.check)
	}
}

func assertNotReady(c *C, chg *state.Change) {
	select {
	case <-chg.Ready():
		c.Fatalf("change %q unexpectedly ready", chg.Summary())
	default:
	}
}

// startServicesNoWait creates a change to start the services, without waiting
// for it to be ready.
func (s *S) startServicesNoWait(c *C, lanes [][]string) *state.Change {
	s.st.Lock()
	ts, err := servstate.Start(s.st, lanes)
	c.Assert(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()
	s.runner.Ensure()
	return chg
}

const latchLayer = `
services:
    latch1:
        override: replace
        command: %s
        on-failure: ignore
        on-check-success:
            chk1: latch
            chk2: latch
            chk3: ignore
checks:
    chk1:
        override: replace
        exec:
            command: "true"
    chk2:
        override: replace
        startup: disabled
        exec:
            command: "true"
    chk3:
        override: replace
        exec:
            command: "true"
`

func (s *S) TestLatchChecks(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiter()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})

	// Only the latch checks are waited for, not the "ignore" one.
	waits := w.next(c, 2)
	c.Assert(waits[0].check, Equals, "chk1")
	c.Assert(waits[1].check, Equals, "chk2")

	svc := s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusActive)
	c.Check(svc.Latching, Equals, true)

	// One check passing isn't enough to latch.
	waits[0].result <- nil
	time.Sleep(50 * time.Millisecond)
	w.assertNoMoreWaits(c)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	assertNotReady(c, chg)

	// Once all latch checks pass, the service is running and the start
	// change completes.
	waits[1].result <- nil
	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	svc = s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusActive)
	c.Check(svc.Latching, Equals, false)

	// Latching cancels the waits.
	waitCanceled(c, waits[0])
	waitCanceled(c, waits[1])
}

func (s *S) TestLatchStopWhileLatching(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiter()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waits := w.next(c, 2)

	stopChg := s.stopServices(c, [][]string{{"latch1"}})
	s.st.Lock()
	c.Check(stopChg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", stopChg.Err()))
	s.st.Unlock()

	waitCanceled(c, waits[0])
	waitCanceled(c, waits[1])

	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*stopped before latch checks passed.*`)
	s.st.Unlock()
}

func (s *S) TestLatchExitWhileLatching(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiter()
	exitPath := filepath.Join(s.dir, "exit")
	command := fmt.Sprintf(`/bin/sh -c "while [ ! -f %s ]; do sleep 0.01; done; exit 3"`, exitPath)
	s.planAddLayer(c, fmt.Sprintf(latchLayer, command))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waits := w.next(c, 2)

	// Make the service exit while it's latching.
	err := os.WriteFile(exitPath, nil, 0o644)
	c.Assert(err, IsNil)

	waitCanceled(c, waits[0])
	waitCanceled(c, waits[1])

	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*exited while latching with code 3, will ignore.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusError)
	c.Check(svc.Latching, Equals, false)
}

func (s *S) TestLatchStaleSuccessIgnored(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiter()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	// Start the service, then stop it while it's latching.
	chg1 := s.startServicesNoWait(c, [][]string{{"latch1"}})
	oldWaits := w.next(c, 2)
	s.stopServices(c, [][]string{{"latch1"}})
	waitChangeReady(c, s.runner, chg1, "Start test")

	// Start it again, so it's latching for a second time.
	chg2 := s.startServicesNoWait(c, [][]string{{"latch1"}})
	newWaits := w.next(c, 2)

	// Successes from the first latching period arrive late, and must not
	// count towards the second.
	oldWaits[0].result <- nil
	oldWaits[1].result <- nil
	time.Sleep(50 * time.Millisecond)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	assertNotReady(c, chg2)

	newWaits[0].result <- nil
	newWaits[1].result <- nil
	waitChangeReady(c, s.runner, chg2, "Start test")
	s.st.Lock()
	c.Check(chg2.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg2.Err()))
	s.st.Unlock()
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, false)
}

func (s *S) TestLatchCheckWaitError(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiter()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waits := w.next(c, 2)

	// A wait that fails (rather than being canceled) doesn't latch the
	// service, even if the other check passes.
	waits[0].result <- fmt.Errorf("check gone")
	waits[1].result <- nil
	time.Sleep(50 * time.Millisecond)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	assertNotReady(c, chg)

	s.stopServices(c, [][]string{{"latch1"}})
	waitChangeReady(c, s.runner, chg, "Start test")
}

func (s *S) TestLatchNoCheckWaiter(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})

	// Without a check waiter the service fails closed: it stays latching
	// instead of being considered running.
	s.waitUntilService(c, "latch1", func(svc *servstate.ServiceInfo) bool {
		return svc.Latching
	})
	time.Sleep(50 * time.Millisecond)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	assertNotReady(c, chg)

	s.stopServices(c, [][]string{{"latch1"}})
	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*stopped before latch checks passed.*`)
	s.st.Unlock()
}
