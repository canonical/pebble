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

// fakeCheckWaiters gives us `pendingCheckWait`s on a channel as they
// are started with `waitForSuccess`. Production does not have an
// equivalent structure -- in-flight waiters are tracked implicitly by
// goroutines parked in real CheckWaiters. When you receive on the
// `waiters` channel, this represents the function call being made,
// and when you send on its `result` channel, this models the function
// returning (you determine the value it returns). Waits deliberately
// do not return when their ctx is canceled, so that tests can deliver
// a result after the service has left the latching state.
type fakeCheckWaiters struct {
	waiters chan *pendingCheckWait
	closed  chan struct{}
}

// pendingCheckWait represents a single in-progress call to a
// `CheckWaiter`. It effectively models a "stack frame" for the
// `CheckWaiter` call -- `check` and `ctx` are the inputs, and
// `result` receives the return value.
type pendingCheckWait struct {
	check  string
	ctx    context.Context
	result chan error
}

// newFakeCheckWaiters creates a fakeCheckWaiters and sets its
// waitForSuccess method as the service manager's check waiter. Any
// waiters still in progress at the end of the test are released.
func (s *S) newFakeCheckWaiters() *fakeCheckWaiters {
	w := &fakeCheckWaiters{
		waiters: make(chan *pendingCheckWait, 10),
		closed:  make(chan struct{}),
	}
	s.AddCleanup(func() { close(w.closed) })
	s.manager.SetCheckWaiter(w.waitForSuccess)
	return w
}

// waitForSuccess is the mock CheckWaiter. Unlike the real
// CheckWaiter, we can control the value returned here by sending on
// the pendingCheckWait's `result` channel.
func (w *fakeCheckWaiters) waitForSuccess(ctx context.Context, check string) error {
	lw := &pendingCheckWait{check: check, ctx: ctx, result: make(chan error, 1)}
	w.waiters <- lw
	select {
	case err := <-lw.result:
		return err
	case <-w.closed:
		return context.Canceled
	}
}

// next returns the next n waiters, sorted by check name.
func (w *fakeCheckWaiters) next(c *C, n int) []*pendingCheckWait {
	var waiters []*pendingCheckWait
	for range n {
		select {
		case lw := <-w.waiters:
			waiters = append(waiters, lw)
		case <-time.After(10 * time.Second):
			c.Fatalf("timed out waiting for latch check waiters (got %d of %d)", len(waiters), n)
		}
	}
	sort.Slice(waiters, func(i, j int) bool { return waiters[i].check < waiters[j].check })
	return waiters
}

// assertNoMoreWaits asserts that no further waiters have been started.
func (w *fakeCheckWaiters) assertNoMoreWaits(c *C) {
	select {
	case lw := <-w.waiters:
		c.Fatalf("unexpected wait for latch check %q", lw.check)
	default:
	}
}

func waiterCanceled(c *C, lw *pendingCheckWait) {
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

// startServicesNoWait creates a change to start the services, without
// waiting for it to be ready.
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
	w := s.newFakeCheckWaiters()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})

	// Only the latch checks are waited for, not the "ignore" one.
	waiters := w.next(c, 2)
	c.Assert(waiters[0].check, Equals, "chk1")
	c.Assert(waiters[1].check, Equals, "chk2")

	svc := s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusActive)
	c.Check(svc.Latching, Equals, true)

	// One check passing isn't enough to latch.
	waiters[0].result <- nil
	time.Sleep(50 * time.Millisecond)
	w.assertNoMoreWaits(c)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	assertNotReady(c, chg)

	// Once all latch checks pass, the service is running and the start
	// change completes.
	waiters[1].result <- nil
	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	svc = s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusActive)
	c.Check(svc.Latching, Equals, false)

	// Latching cancels the waiters.
	waiterCanceled(c, waiters[0])
	waiterCanceled(c, waiters[1])
}

func (s *S) TestLatchStopWhileLatching(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiters()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waiters := w.next(c, 2)

	stopChg := s.stopServices(c, [][]string{{"latch1"}})
	s.st.Lock()
	c.Check(stopChg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", stopChg.Err()))
	s.st.Unlock()

	waiterCanceled(c, waiters[0])
	waiterCanceled(c, waiters[1])

	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*stopped before latch checks passed.*`)
	s.st.Unlock()
}

func (s *S) TestLatchExitWhileLatching(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiters()
	exitPath := filepath.Join(s.dir, "exit")
	command := fmt.Sprintf(`/bin/sh -c "while [ ! -f %s ]; do sleep 0.01; done; exit 3"`, exitPath)
	s.planAddLayer(c, fmt.Sprintf(latchLayer, command))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waiters := w.next(c, 2)

	// Make the service exit while it's latching.
	err := os.WriteFile(exitPath, nil, 0o644)
	c.Assert(err, IsNil)

	waiterCanceled(c, waiters[0])
	waiterCanceled(c, waiters[1])

	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*exited while latching with code 3, will ignore.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "latch1")
	c.Check(svc.Current, Equals, servstate.StatusError)
	c.Check(svc.Latching, Equals, false)
}

func (s *S) TestLatchRestartAfterExitDuringLatching(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiters()
	exitPath := filepath.Join(s.dir, "exit")
	// The service waits for the exit file, removes it, and exits with a
	// failure. The restarted instance finds no file, so it stays up long
	// enough to pass the okay delay and re-latch.
	command := fmt.Sprintf(`/bin/sh -c "while [ ! -f %s ]; do sleep 0.01; done; rm %s; exit 3"`, exitPath, exitPath)
	s.planAddLayer(c, fmt.Sprintf(`
services:
    latch1:
        override: replace
        command: %s
        on-failure: restart
        backoff-delay: 10ms
        on-check-success:
            chk1: latch
checks:
    chk1:
        override: replace
        startup: disabled
        exec:
            command: "true"
`, command))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waiters1 := w.next(c, 1)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)

	// Make the service exit while it's latching. With on-failure: restart
	// this fails the start task and puts the service into backoff.
	err := os.WriteFile(exitPath, nil, 0o644)
	c.Assert(err, IsNil)
	waiterCanceled(c, waiters1[0])

	waitChangeReady(c, s.runner, chg, "Start test")
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*exited while latching with code 3, will restart.*`)
	s.st.Unlock()
	c.Check(s.manager.BackoffNum("latch1"), Equals, 1)

	// After the backoff, the service must be brought up again from
	// scratch, since it did not finish latching before the restart.
	waiters2 := w.next(c, 1)
	c.Check(s.serviceByName(c, "latch1").Latching, Equals, true)
	c.Check(s.manager.BackoffNum("latch1"), Equals, 1)

	waiters2[0].result <- nil
	s.waitUntilService(c, "latch1", func(svc *servstate.ServiceInfo) bool {
		return !svc.Latching && svc.Current == servstate.StatusActive
	})
	// Check that there was not another backoff after the first
	c.Check(s.manager.BackoffNum("latch1"), Equals, 1)
}

func (s *S) TestLatchStaleSuccessIgnored(c *C) {
	s.newServiceManager(c)
	w := s.newFakeCheckWaiters()
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
	w := s.newFakeCheckWaiters()
	s.planAddLayer(c, fmt.Sprintf(latchLayer, "sleep 10"))
	s.planChanged(c)

	chg := s.startServicesNoWait(c, [][]string{{"latch1"}})
	waiters := w.next(c, 2)

	// A wait that fails (rather than being canceled) doesn't latch the
	// service, even if the other check passes.
	waiters[0].result <- fmt.Errorf("check gone")
	waiters[1].result <- nil
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
