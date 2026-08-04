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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
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

// scheduleChange returns the (single, expected) service-schedule change, or
// nil if none exists.
func (s *S) scheduleChange(c *C) *state.Change {
	s.st.Lock()
	defer s.st.Unlock()
	for _, chg := range s.st.Changes() {
		if chg.Kind() == servstate.ServiceScheduleKind {
			return chg
		}
	}
	return nil
}

func (s *S) scheduleDetails(c *C, chg *state.Change) scheduleDetails {
	s.st.Lock()
	defer s.st.Unlock()
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)
	var details scheduleDetails
	err := tasks[0].Get(servstate.ScheduleDetailsAttr, &details)
	c.Assert(err, IsNil)
	return details
}

func (s *S) setScheduleNext(c *C, chg *state.Change, next time.Time) {
	s.st.Lock()
	defer s.st.Unlock()
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)
	var details scheduleDetails
	err := tasks[0].Get(servstate.ScheduleDetailsAttr, &details)
	c.Assert(err, IsNil)
	details.Next = next
	tasks[0].Set(servstate.ScheduleDetailsAttr, &details)
}

func (s *S) scheduleTaskLog(c *C, chg *state.Change) []string {
	s.st.Lock()
	defer s.st.Unlock()
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)
	return tasks[0].Log()
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

// -- PlanChanged behaviour --

func (s *S) TestScheduleCreatedOnPlanChanged(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)

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
	s.st.Unlock()
	c.Check(ready, Equals, true)
	c.Check(status, Equals, state.HoldStatus)

	// No new schedule change should have been created for the service.
	c.Check(s.countChangesOfKind(c, servstate.ServiceScheduleKind), Equals, 1)
}

// -- Ensure behaviour --

func (s *S) TestEnsureStartsServiceOnSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)

	// Force the schedule to be due right now.
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	err := s.manager.Ensure()
	c.Assert(err, IsNil)

	startChg := s.findChangeOfKind(c, "start")
	c.Assert(startChg, NotNil)
	waitChangeReady(c, s.runner, startChg, "service to start on schedule")

	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	logs := s.scheduleTaskLog(c, chg)
	c.Check(logContains(logs, "Started service"), Equals, true)
}

func (s *S) TestEnsureLogsMissedScheduleButStillRuns(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)

	// Missed by 30 seconds (well over the "missed" logging threshold), but
	// still much closer to now than the next (daily) occurrence, so it
	// should run anyway.
	s.setScheduleNext(c, chg, time.Now().Add(-30*time.Second))

	err := s.manager.Ensure()
	c.Assert(err, IsNil)

	startChg := s.findChangeOfKind(c, "start")
	c.Assert(startChg, NotNil)
	waitChangeReady(c, s.runner, startChg, "service to start on schedule")

	s.waitUntilService(c, "sched1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	logs := s.scheduleTaskLog(c, chg)
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
	s.setScheduleNext(c, chg, time.Now().Add(-time.Second))

	err := s.manager.Ensure()
	c.Assert(err, IsNil)

	// No "start" change should have been created by the schedule.
	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)

	logs := s.scheduleTaskLog(c, chg)
	c.Check(logContains(logs, "already running"), Equals, true)
}

func (s *S) TestEnsureSkipsFarMissedSchedule(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, scheduleTestLayer)
	s.planChanged(c)

	chg := s.scheduleChange(c)
	c.Assert(chg, NotNil)

	longAgo := time.Now().Add(-240 * time.Hour) // 10 days ago
	s.setScheduleNext(c, chg, longAgo)

	err := s.manager.Ensure()
	c.Assert(err, IsNil)

	// No service should have been started because of this.
	c.Check(s.countChangesOfKind(c, "start"), Equals, 0)

	details := s.scheduleDetails(c, chg)
	// Rescheduled well into the future relative to the missed time (the
	// schedule fires daily, so the new Next should be close to now, not
	// close to the 10-day-old missed time).
	c.Check(details.Next.After(longAgo.Add(48*time.Hour)), Equals, true)

	logs := s.scheduleTaskLog(c, chg)
	c.Check(logContains(logs, "Skipped scheduled start"), Equals, true)
}

// findChangeOfKind returns the first change of the given kind not equal to
// any service-schedule change, or nil if none is found.
func (s *S) findChangeOfKind(c *C, kind string) *state.Change {
	s.st.Lock()
	defer s.st.Unlock()
	for _, chg := range s.st.Changes() {
		if chg.Kind() == kind {
			return chg
		}
	}
	return nil
}
