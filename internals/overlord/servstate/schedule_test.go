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
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// scheduleDetails mirrors the JSON shape of servstate's internal
// scheduleDetails type, so tests can inspect/mutate it via the task's
// generic Get/Set without needing access to the unexported type itself.
type scheduleDetails struct {
	ServiceName string    `json:"service-name"`
	Schedule    string    `json:"schedule"`
	Next        time.Time `json:"next"`
}

const scheduleTestLayer = `
services:
    sched1:
        override: replace
        command: /bin/sh -c "sleep 10"
        schedule: 9:00-11:00
`

// scheduleChange returns the pending (not yet ready) service-schedule
// change, or nil if none exists. There should be at most one such change per
// service at any given time, and it persists (rather than being recreated)
// for as long as the service has a schedule.
func (s *S) scheduleChange(c *C) *state.Change {
	s.st.Lock()
	defer s.st.Unlock()
	for _, chg := range s.st.Changes() {
		if chg.Kind() == servstate.ServiceScheduleKind && !chg.IsReady() {
			return chg
		}
	}
	return nil
}

// scheduleTask returns the task in chg that's still waiting for its
// scheduled time to arrive (i.e. the one tracking the service's next
// scheduled occurrence). It fails the test if there's no such task.
func (s *S) scheduleTask(c *C, chg *state.Change) *state.Task {
	s.st.Lock()
	defer s.st.Unlock()
	for _, t := range chg.Tasks() {
		if t.Status() == state.DoStatus && t.Has(servstate.ScheduleDetailsAttr) {
			return t
		}
	}
	c.Fatalf("no pending schedule task found in change %s", chg.ID())
	return nil
}

func (s *S) scheduleDetails(c *C, chg *state.Change) scheduleDetails {
	task := s.scheduleTask(c, chg)
	s.st.Lock()
	defer s.st.Unlock()
	var details scheduleDetails
	err := task.Get(servstate.ScheduleDetailsAttr, &details)
	c.Assert(err, IsNil)
	return details
}

func (s *S) setScheduleNext(c *C, chg *state.Change, next time.Time) {
	task := s.scheduleTask(c, chg)
	s.st.Lock()
	defer s.st.Unlock()
	var details scheduleDetails
	err := task.Get(servstate.ScheduleDetailsAttr, &details)
	c.Assert(err, IsNil)
	details.Next = next
	task.Set(servstate.ScheduleDetailsAttr, &details)
	task.At(next)
}

// waitTaskLogContains runs the task runner until the task's log contains the
// sub-string, or fails the test after a timeout.
func waitTaskLogContains(c *C, runner *state.TaskRunner, st *state.State, task *state.Task, substr string) {
	timeout := time.After(10 * time.Second)
	for {
		runner.Ensure()
		st.Lock()
		found := logContains(task.Log(), substr)
		st.Unlock()
		if found {
			return
		}
		select {
		case <-timeout:
			c.Fatalf("timeout waiting for task log to contain %q", substr)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// waitTaskReady runs the task runner until the task's status becomes Ready
// (Done, Error, etc), or fails the test after a timeout.
func waitTaskReady(c *C, runner *state.TaskRunner, st *state.State, task *state.Task) {
	timeout := time.After(10 * time.Second)
	for {
		runner.Ensure()
		st.Lock()
		ready := task.Status().Ready()
		st.Unlock()
		if ready {
			return
		}
		select {
		case <-timeout:
			c.Fatalf("timeout waiting for task %s to become ready", task.ID())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (s *S) scheduleTaskLog(c *C, chg *state.Change) []string {
	task := s.scheduleTask(c, chg)
	s.st.Lock()
	defer s.st.Unlock()
	return task.Log()
}

func (s *S) countChangesOfKind(c *C, kind string) int {
	s.st.Lock()
	defer s.st.Unlock()
	n := 0
	for _, chg := range s.st.Changes() {
		if chg.Kind() == kind {
			n++
		}
	}
	return n
}

func logContains(logs []string, substr string) bool {
	for _, l := range logs {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// -- Pure decision function --

func (s *S) TestScheduleShouldRunNow(c *C) {
	now := time.Now()

	// Missed by a minute, next occurrence an hour away: closer to the
	// missed time, so run now.
	c.Check(servstate.ScheduleShouldRunNow(now, now.Add(-time.Minute), now.Add(time.Hour)), Equals, true)

	// Missed by 10 days, next occurrence an hour away: closer to the next
	// occurrence, so skip.
	c.Check(servstate.ScheduleShouldRunNow(now, now.Add(-10*24*time.Hour), now.Add(time.Hour)), Equals, false)

	// Equidistant: favour running now.
	c.Check(servstate.ScheduleShouldRunNow(now, now.Add(-time.Hour), now.Add(time.Hour)), Equals, true)
}

func (s *S) TestNextScheduleTimeInvalid(c *C) {
	_, err := servstate.NextScheduleTime("not-a-schedule", time.Now())
	c.Assert(err, NotNil)
}

// TestNextScheduleTimeAfterAlwaysAdvances guards against a schedule
// computation getting "stuck": for a schedule with a spread (randomised)
// window that's currently open, re-deriving the next occurrence from a
// previous occurrence can otherwise land back inside the same still-open
// window with a new random offset, so it never actually advances into the
// future. That would cause a scheduled start's "next" time to be treated as
// immediately due again and again, spawning an unbounded chain of
// service-schedule changes.
func (s *S) TestNextScheduleTimeAfterAlwaysAdvances(c *C) {
	now := time.Now()
	// A schedule with a spread window that's open right now: re-evaluating
	// from a previous occurrence can otherwise land back inside the same
	// still-open window with a new random offset.
	sched := fmt.Sprintf("%02d:%02d-%02d:%02d/2", now.Hour(), now.Minute(), (now.Hour()+1)%24, now.Minute())

	last := now.Add(-24 * time.Hour)
	for i := 0; i < 50; i++ {
		next, err := servstate.NextScheduleTimeAfter(sched, last)
		c.Assert(err, IsNil)
		c.Assert(next.After(time.Now()), Equals, true,
			Commentf("iteration %d: next=%v is not after now", i, next))
		last = next
	}
}

// -- PlanChanged behaviour --

func (s *S) TestScheduleCreatedOnPlanChanged(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	c.Check(chg.Summary(), Equals, `Schedule service "sched1"`)

	details := s.scheduleDetails(c, chg)
	c.Check(details.ServiceName, Equals, "sched1")
	c.Check(details.Schedule, Equals, "9:00-11:00")
	c.Check(details.Next.IsZero(), Equals, false)
	// The schedule fires daily, so the next occurrence should always be
	// within a day of now.
	c.Check(details.Next.Before(time.Now().Add(25*time.Hour)), Equals, true)
}

func (s *S) TestScheduleNotCreatedWithoutSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    plain1:
        override: replace
        command: /bin/sh -c "sleep 10"
`)
	s.planChanged(c)

	c.Check(s.scheduleChange(c), IsNil)
}

func (s *S) TestScheduleUnchangedKeepsNext(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	details1 := s.scheduleDetails(c, chg)

	// Re-applying the same plan shouldn't touch the next scheduled time,
	// nor create a second change.
	s.planChanged(c)

	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	chg2 := s.scheduleChange(c)
	c.Assert(chg2.ID(), Equals, chg.ID())
	details2 := s.scheduleDetails(c, chg2)
	c.Check(details2.Next.Equal(details1.Next), Equals, true)
}

func (s *S) TestScheduleChangedUpdatesNextAndReusesChange(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	s.planAddLayer(c, `
services:
    sched1:
        override: merge
        schedule: 13:00-15:00
`)
	s.planChanged(c)

	// Same change/task should have been reused, not a new one.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	chg2 := s.scheduleChange(c)
	c.Assert(chg2.ID(), Equals, chg.ID())
	task2 := s.scheduleTask(c, chg2)
	c.Check(task2.ID(), Equals, task.ID())

	details2 := s.scheduleDetails(c, chg2)
	c.Check(details2.Schedule, Equals, "13:00-15:00")

	logs := s.scheduleTaskLog(c, chg2)
	c.Check(logContains(logs, "Schedule for service"), Equals, true)
}

func (s *S) TestScheduleRemovedRetiresChange(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	// Replace the service definition with one that has no schedule.
	s.planAddLayer(c, `
services:
    sched1:
        override: replace
        command: /bin/sh -c "sleep 10"
`)
	s.planChanged(c)

	s.st.Lock()
	ready := chg.IsReady()
	status := chg.Status()
	logs := task.Log()
	s.st.Unlock()
	c.Check(ready, Equals, true)
	c.Check(status, Equals, state.DoneStatus)
	c.Check(logContains(logs, "unscheduled"), Equals, true)

	// No new schedule change should have been created for the service.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
}

func (s *S) TestScheduleRemovedWithServiceRetiresChange(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	// Remove the service entirely from the plan (rather than merely
	// dropping its schedule) by feeding the manager a plan that doesn't
	// mention it at all.
	emptyPlan := &plan.Plan{
		Services: map[string]*plan.Service{},
	}
	s.manager.PlanChanged(emptyPlan)

	s.st.Lock()
	status := chg.Status()
	logs := task.Log()
	s.st.Unlock()
	c.Check(status, Equals, state.DoneStatus)
	c.Check(logContains(logs, "unscheduled"), Equals, true)
	c.Check(logContains(logs, "removed from the plan"), Equals, true)
}

// -- Ensure behaviour --

func (s *S) TestEnsureStartsServiceOnSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	// Force the schedule to be due right now.
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	waitTaskReady(c, s.runner, s.st, task)

	// No independent "start" change should have been created; the start
	// task lives directly in the persistent schedule change.
	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)

	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	s.st.Lock()
	logs := task.Log()
	taskStatus := task.Status()
	s.st.Unlock()
	c.Check(logContains(logs, "Started service"), Equals, true)
	c.Check(taskStatus, Equals, state.DoneStatus)

	// The same persistent change should be used to track the next
	// occurrence, rather than a new one.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	newChg := s.scheduleChange(c)
	c.Assert(newChg, NotNil)
	c.Check(newChg.ID(), Equals, chg.ID())

	newTask := s.scheduleTask(c, newChg)
	c.Check(newTask.ID() != task.ID(), Equals, true)

	s.st.Lock()
	ready := chg.IsReady()
	s.st.Unlock()
	c.Check(ready, Equals, false)
}

func (s *S) TestEnsureLogsMissedScheduleButStillRuns(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	// Missed by 30 seconds (well over the "missed" logging threshold), but
	// still much closer to now than the next (daily) occurrence, so it
	// should run anyway.
	s.setScheduleNext(c, chg, time.Now().Add(-30*time.Second))

	waitTaskReady(c, s.runner, s.st, task)

	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)

	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	s.st.Lock()
	logs := task.Log()
	s.st.Unlock()
	c.Check(logContains(logs, "Missed scheduled start"), Equals, true)
}

func (s *S) TestEnsureSkipsStartWhenAlreadyRunning(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	// Start the service manually first.
	s.startServices(c, [][]string{{"sched1"}})
	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	waitTaskLogContains(c, s.runner, s.st, task, "already running")
	waitTaskReady(c, s.runner, s.st, task)

	// No "start" change should have been created by the schedule, and the
	// schedule change should still be pending (not finished), since a new
	// task should have been queued for the next occurrence.
	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)
	s.st.Lock()
	ready := chg.IsReady()
	taskStatus := task.Status()
	s.st.Unlock()
	c.Check(ready, Equals, false)
	c.Check(taskStatus, Equals, state.DoneStatus)

	newTask := s.scheduleTask(c, chg)
	c.Check(newTask.ID() != task.ID(), Equals, true)
}

func (s *S) TestEnsureSkipsFarMissedSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	longAgo := time.Now().Add(-240 * time.Hour) // 10 days ago
	s.setScheduleNext(c, chg, longAgo)

	waitTaskLogContains(c, s.runner, s.st, task, "Skipped scheduled start")
	waitTaskReady(c, s.runner, s.st, task)

	// No service should have been started because of this.
	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)

	details := s.scheduleDetails(c, chg)
	// Rescheduled well into the future relative to the missed time (the
	// schedule fires daily, so the new Next should be close to now, not
	// close to the 10-day-old missed time).
	c.Check(details.Next.After(longAgo.Add(48*time.Hour)), Equals, true)
}

// TestScheduleHistoryPruned checks that a service-schedule change never
// accumulates more than MaxScheduleHistory finished tasks, alongside the one
// pending task tracking the next occurrence.
func (s *S) TestScheduleHistoryPruned(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)

	for i := 0; i < servstate.MaxScheduleHistory+2; i++ {
		task := s.scheduleTask(c, chg)
		s.setScheduleNext(c, chg, time.Now().Add(-time.Second))
		waitTaskReady(c, s.runner, s.st, task)
	}

	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	s.st.Lock()
	tasks := chg.Tasks()
	var finished, pending int
	for _, t := range tasks {
		if t.Status().Ready() {
			finished++
		} else {
			pending++
		}
	}
	s.st.Unlock()

	c.Check(pending, Equals, 1)
	c.Check(finished, Equals, servstate.MaxScheduleHistory)
}
