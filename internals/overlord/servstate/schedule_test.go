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

	"github.com/canonical/pebble/internals/overlord/planstate"
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
	Last        time.Time `json:"last,omitzero"`
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
		status := t.Status()
		if (status == state.DoStatus || status == state.WaitStatus) && t.Has(servstate.ScheduleDetailsAttr) {
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
// computation getting "stuck": for a split schedule span that's currently
// open, re-deriving the next occurrence from a previous occurrence can
// otherwise land on a part of the span that started before now. That would
// cause a scheduled start's "next" time to be treated as immediately due
// again and again, spawning an unbounded chain of service-schedule changes.
func (s *S) TestNextScheduleTimeAfterAlwaysAdvances(c *C) {
	now := time.Now()
	// A split span that's open right now: re-evaluating from a previous
	// occurrence can otherwise land on a part of the span that's in the past.
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

func (s *S) TestScheduleWithoutNextOccurrenceIsDormant(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    sched1:
        override: replace
        command: /bin/sh -c "sleep 10"
        schedule: feb,30
`)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)

	// Ensuring must not run a schedule which has no next occurrence.
	s.runner.Ensure()
	s.st.Lock()
	status := task.Status()
	at := task.AtTime()
	s.st.Unlock()
	c.Check(status, Equals, state.WaitStatus)
	c.Check(at.IsZero(), Equals, true)
	c.Check(s.scheduleDetails(c, chg).Next.IsZero(), Equals, true)
	c.Check(s.manager.RunningCmds(), HasLen, 0)

	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Current, Equals, servstate.StatusInactive)
	c.Check(services[0].ScheduleNext.IsZero(), Equals, true)
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

func (s *S) TestScheduleChangedWithoutNextOccurrenceIsDormant(c *C) {
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
        schedule: feb,30
`)
	s.planChanged(c)

	// Reuse the existing change and task, but leave the task dormant rather
	// than clearing its At time and making it immediately runnable.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	c.Check(s.scheduleChange(c).ID(), Equals, chg.ID())
	c.Check(s.scheduleTask(c, chg).ID(), Equals, task.ID())

	s.runner.Ensure()
	s.st.Lock()
	status := task.Status()
	s.st.Unlock()
	c.Check(status, Equals, state.WaitStatus)
	c.Check(s.scheduleDetails(c, chg).Next.IsZero(), Equals, true)
	c.Check(s.manager.RunningCmds(), HasLen, 0)

	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Current, Equals, servstate.StatusInactive)
	c.Check(services[0].ScheduleNext.IsZero(), Equals, true)
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

func (s *S) TestSchedulePlanNotificationsSerialized(c *C) {
	s.newServiceManager(c)
	planManager, err := planstate.NewManager(c.MkDir())
	c.Assert(err, IsNil)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	planManager.AddChangeListener(func(p *plan.Plan) {
		if len(p.Layers) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		s.manager.PlanChanged(p)
	})

	firstLayer, err := plan.ParseLayer(0, "first", []byte(scheduleTestLayer))
	c.Assert(err, IsNil)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- planManager.AppendLayer(firstLayer, false)
	}()
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		c.Fatal("timed out waiting for first plan listener")
	}

	secondLayer, err := plan.ParseLayer(0, "second", []byte(`
services:
    sched1:
        override: merge
        schedule: 13:00-15:00
`))
	c.Assert(err, IsNil)
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- planManager.AppendLayer(secondLayer, false)
	}()

	// The second update cannot commit or notify while the first update's
	// listener is blocked. Without serialized notifications, it would apply
	// the final plan first and the older callback would then roll it back.
	select {
	case err := <-secondDone:
		close(releaseFirst)
		c.Assert(<-firstDone, IsNil)
		c.Fatalf("second plan update completed while first listener was blocked: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	c.Assert(<-firstDone, IsNil)
	c.Assert(<-secondDone, IsNil)
	s.plan = planManager.Plan()
	s.planPropagated = true

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	c.Check(s.scheduleDetails(c, chg).Schedule, Equals, "13:00-15:00")
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)

	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Schedule, Equals, s.plan.Services["sched1"].Schedule)
	c.Check(services[0].Schedule, Equals, "13:00-15:00")
}

func (s *S) TestScheduleChangedWhileDoingUsesLatestPlan(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	// Model the window after TaskRunner has moved the due task to Doing,
	// but before doStart has processed its stored schedule details.
	s.st.Lock()
	task.SetStatus(state.DoingStatus)
	s.st.Unlock()

	s.planAddLayer(c, `
services:
    sched1:
        override: merge
        schedule: 13:00-15:00
`)
	s.planChanged(c)

	waitTaskReady(c, s.runner, s.st, task)

	// The old occurrence must not start the service or enqueue another old
	// schedule, and PlanChanged must not have created a duplicate change.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	c.Check(s.manager.RunningCmds(), HasLen, 0)
	details := s.scheduleDetails(c, chg)
	c.Check(details.Schedule, Equals, "13:00-15:00")
	c.Check(details.Last.IsZero(), Equals, true)

	s.st.Lock()
	changeReady := chg.IsReady()
	logs := task.Log()
	s.st.Unlock()
	c.Check(changeReady, Equals, false)
	c.Check(logContains(logs, `changed from "9:00-11:00" to "13:00-15:00"`), Equals, true)
}

func (s *S) TestScheduleRemovedWhileDoingDoesNotStartOrReschedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	s.st.Lock()
	task.SetStatus(state.DoingStatus)
	s.st.Unlock()

	s.planAddLayer(c, `
services:
    sched1:
        override: replace
        command: /bin/sh -c "sleep 10"
`)
	s.planChanged(c)

	waitTaskReady(c, s.runner, s.st, task)

	c.Check(s.manager.RunningCmds(), HasLen, 0)
	s.st.Lock()
	tasks := chg.Tasks()
	changeStatus := chg.Status()
	logs := task.Log()
	s.st.Unlock()
	c.Check(tasks, HasLen, 1)
	c.Check(changeStatus, Equals, state.DoneStatus)
	c.Check(logContains(logs, "unscheduled"), Equals, true)
}

// pauseScheduledStart makes the next scheduled start pause after it has been
// checked against the plan, but before the start is initiated. It runs the
// task runner until the start is paused there, and returns a function that
// resumes it.
func (s *S) pauseScheduledStart(c *C) (resume func()) {
	paused := make(chan struct{})
	resumed := make(chan struct{})
	restore := servstate.FakeScheduledStartPrepared(func() {
		close(paused)
		<-resumed
	})
	s.AddCleanup(restore)

	timeout := time.After(10 * time.Second)
	for {
		s.runner.Ensure()
		select {
		case <-paused:
			return func() { close(resumed) }
		case <-timeout:
			close(resumed)
			c.Fatal("timed out waiting for scheduled start to be checked against the plan")
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *S) TestScheduleChangedAfterCheckDoesNotStart(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	// The occurrence passes the plan check and is about to start...
	resume := s.pauseScheduledStart(c)

	// ...when a plan update supersedes its schedule.
	s.planAddLayer(c, `
services:
    sched1:
        override: merge
        schedule: 13:00-15:00
`)
	s.planChanged(c)

	resume()
	waitTaskReady(c, s.runner, s.st, task)

	// The stale occurrence must not have started the service.
	c.Check(s.manager.Config("sched1"), IsNil)
	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Current, Equals, servstate.StatusInactive)

	// The following occurrence tracks the new schedule, in the same change.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
	details := s.scheduleDetails(c, chg)
	c.Check(details.Schedule, Equals, "13:00-15:00")

	s.st.Lock()
	taskStatus := task.Status()
	changeReady := chg.IsReady()
	logs := task.Log()
	s.st.Unlock()
	c.Check(taskStatus, Equals, state.DoneStatus)
	c.Check(changeReady, Equals, false)
	c.Check(logContains(logs, `changed from "9:00-11:00" to "13:00-15:00"; skipped scheduled start`), Equals, true)
}

func (s *S) TestScheduleRemovedAfterCheckDoesNotStart(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	resume := s.pauseScheduledStart(c)

	s.planAddLayer(c, `
services:
    sched1:
        override: replace
        command: /bin/sh -c "sleep 10"
`)
	s.planChanged(c)

	resume()
	waitTaskReady(c, s.runner, s.st, task)

	c.Check(s.manager.Config("sched1"), IsNil)
	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Current, Equals, servstate.StatusInactive)

	// The schedule is retired, including the occurrence queued to follow the
	// stale one.
	c.Check(s.scheduleChange(c), IsNil)
	s.st.Lock()
	changeStatus := chg.Status()
	pending := 0
	for _, t := range chg.Tasks() {
		if !t.Status().Ready() {
			pending++
		}
	}
	logs := task.Log()
	s.st.Unlock()
	c.Check(changeStatus, Equals, state.DoneStatus)
	c.Check(pending, Equals, 0)
	c.Check(logContains(logs, "unscheduled"), Equals, true)
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
	taskStatus := task.Status()
	s.st.Unlock()
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

func (s *S) TestEnsureFailedStartKeepsNextSchedulePending(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    sched1:
        override: replace
        command: /bin/sh -c "exit 1"
        on-failure: ignore
        schedule: 9:00-11:00
`)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	task := s.scheduleTask(c, chg)
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	waitTaskReady(c, s.runner, s.st, task)

	s.st.Lock()
	status := task.Status()
	var next *state.Task
	for _, candidate := range chg.Tasks() {
		if candidate.ID() != task.ID() && candidate.Has(servstate.ScheduleDetailsAttr) {
			next = candidate
			break
		}
	}
	s.st.Unlock()
	c.Check(status, Equals, state.ErrorStatus)
	c.Assert(next, NotNil)

	// A failed occurrence must not abort the successor in the same change.
	s.st.Lock()
	nextStatus := next.Status()
	changeReady := chg.IsReady()
	s.st.Unlock()
	c.Check(nextStatus, Equals, state.DoStatus)
	c.Check(changeReady, Equals, false)
	c.Check(s.scheduleDetails(c, chg).Next.After(time.Now()), Equals, true)
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

	// The schedule still fired, even though the service was already running.
	c.Check(s.scheduleDetails(c, chg).Last.IsZero(), Equals, false)
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
	// The skipped occurrence doesn't count as the schedule firing.
	c.Check(details.Last.IsZero(), Equals, true)
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

func (s *S) TestScheduleLastTracksFiring(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)
	c.Check(s.scheduleDetails(c, chg).Last.IsZero(), Equals, true)

	// Not fired yet: the schedule and next time are reported, but no last.
	services, err := s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Schedule, Equals, "9:00-11:00")
	c.Check(services[0].ScheduleLast.IsZero(), Equals, true)
	c.Check(services[0].ScheduleNext.Equal(s.scheduleDetails(c, chg).Next), Equals, true)

	task := s.scheduleTask(c, chg)
	before := time.Now()
	s.setScheduleNext(c, chg, before.Add(-time.Second))
	waitTaskReady(c, s.runner, s.st, task)
	after := time.Now()

	// The task tracking the following occurrence records when it fired.
	details := s.scheduleDetails(c, chg)
	c.Check(details.Last.Before(before), Equals, false)
	c.Check(details.Last.After(after), Equals, false)

	services, err = s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	c.Check(services[0].Schedule, Equals, "9:00-11:00")
	c.Check(services[0].ScheduleLast.Equal(details.Last), Equals, true)
	c.Check(services[0].ScheduleNext.Equal(details.Next), Equals, true)

	// Changing the schedule keeps the last time it fired.
	s.planAddLayer(c, `
services:
    sched1:
        override: merge
        schedule: 13:00-15:00
`)
	s.planChanged(c)
	changed := s.scheduleDetails(c, chg)
	c.Check(changed.Schedule, Equals, "13:00-15:00")
	c.Check(changed.Last.Equal(details.Last), Equals, true)

	services, err = s.manager.Services([]string{"sched1"})
	c.Assert(err, IsNil)
	c.Check(services[0].Schedule, Equals, "13:00-15:00")
	c.Check(services[0].ScheduleLast.Equal(details.Last), Equals, true)
	c.Check(services[0].ScheduleNext.Equal(changed.Next), Equals, true)
}

func (s *S) TestServicesWithoutSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	services, err := s.manager.Services(nil)
	c.Assert(err, IsNil)
	for _, svc := range services {
		c.Check(svc.Schedule, Equals, "")
		c.Check(svc.ScheduleLast.IsZero(), Equals, true)
		c.Check(svc.ScheduleNext.IsZero(), Equals, true)
	}
}
