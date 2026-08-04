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

package servstate

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/timeutil"
)

const (
	// serviceScheduleKind is the kind used for both the change and the task
	// that tracks a service's next scheduled start time. There is at most one
	// such change per service at any given time.
	serviceScheduleKind = "service-schedule"

	// scheduleDetailsAttr is the task attribute holding scheduleDetails.
	scheduleDetailsAttr = "service-schedule-details"

	// scheduleNoPruneAttr marks the schedule change as one that must never
	// be pruned while it's still tracking a service's schedule.
	scheduleNoPruneAttr = "service-schedule-no-prune"

	// maxScheduleLookahead bounds how far in the future we search for the next
	// scheduled start time.
	maxScheduleLookahead = 366 * 24 * time.Hour

	// scheduleMissThreshold is how overdue a scheduled start has to be before
	// we call it out explicitly as "missed" in the task log.
	scheduleMissThreshold = 5 * time.Second
)

// scheduleDetails is persisted on the service-schedule task, and records the
// schedule string, with the next time the service should be started.
type scheduleDetails struct {
	ServiceName string    `json:"service-name"`
	Schedule    string    `json:"schedule"`
	Next        time.Time `json:"next"`
}

// nextScheduleTime parses the schedule, returning the next time that it fires,
// according to the current time.
func nextScheduleTime(scheduleStr string, last time.Time) (time.Time, error) {
	schedules, err := timeutil.ParseSchedule(scheduleStr)
	if err != nil {
		return time.Time{}, err
	}
	d := timeutil.Next(schedules, last, maxScheduleLookahead)
	return timeNow().Add(d), nil
}

// scheduleShouldRunNow decides whether a scheduled start that was missed should
// still be acted on now, or skipped in favour of waiting for the following
// occurrence.
//
// The decision is based on which of the two candidate times is closer to now.
// If the missed time is closer (or equidistant), we start the service now; if
// the following occurrence is closer, we wait for it instead.
func scheduleShouldRunNow(now, missed, following time.Time) bool {
	if following.IsZero() {
		return now.After(missed)
	}
	missedDelta := max(now.Sub(missed), 0)
	followingDelta := max(following.Sub(now), 0)
	return missedDelta <= followingDelta
}

// serviceScheduleChange creates the change/task pair used to track name's next
// scheduled start time, and returns the change ID.
// The caller must hold the state lock.
func serviceScheduleChange(st *state.State, name, scheduleStr string, next time.Time) string {
	summary := fmt.Sprintf("Wait for scheduled start of service %q", name)
	task := st.NewTask(serviceScheduleKind, summary)
	task.Set(scheduleDetailsAttr, &scheduleDetails{
		ServiceName: name,
		Schedule:    scheduleStr,
		Next:        next,
	})
	// This task is never picked up by a TaskRunner handler (none is
	// registered for this kind); it's driven entirely by
	// ServiceManager.Ensure. Mark it Doing so it's clear from "pebble
	// changes"/"pebble tasks" that it's ongoing.
	task.SetStatus(state.DoingStatus)

	change := st.NewChange(serviceScheduleKind, summary)
	change.Set(scheduleNoPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

// scheduleChanged is called from PlanChanged to create, update, or retire
// the per-service schedule changes/tasks to match the new plan.
func (m *ServiceManager) scheduleChanged(newPlan *plan.Plan) {
	m.state.Lock()
	defer m.state.Unlock()

	shouldEnsure := false
	existing := make(map[string]bool)

	for _, change := range m.state.Changes() {
		if change.Kind() != serviceScheduleKind || change.IsReady() {
			continue
		}
		task := change.Tasks()[0]
		if !task.Has(scheduleDetailsAttr) {
			continue
		}
		var details scheduleDetails
		err := task.Get(scheduleDetailsAttr, &details)
		if err != nil {
			logger.Noticef("Cannot get %s change %s schedule details: %v", change.Kind(), change.ID(), err)
			task.Errorf("Cannot get %s change %s schedule details: %v", change.Kind(), change.ID(), err)
			continue
		}
		existing[details.ServiceName] = true

		config, inPlan := newPlan.Services[details.ServiceName]
		if !inPlan || config.Schedule == "" {
			// Service removed from the plan, or no longer has a schedule:
			// retire this change. We can't use change.Abort() here because
			// there's no TaskRunner handler registered for this task kind to
			// process the resulting Abort/Undo status.
			task.Logf("Service %q no longer has a schedule configured; no more scheduled starts.", details.ServiceName)
			task.SetStatus(state.HoldStatus)
			shouldEnsure = true
			continue
		}

		if config.Schedule == details.Schedule {
			// Schedule hasn't changed.
			continue
		}

		// Schedule string changed: recompute the next scheduled start time, but
		// reuse the existing change/task rather than creating a new one.
		next, err := nextScheduleTime(config.Schedule, details.Next)
		if err != nil {
			logger.Noticef("Cannot parse schedule %q for service %q: %v", config.Schedule, details.ServiceName, err)
			continue
		}
		task.Logf("Schedule for service %q changed from %q to %q; next scheduled start at %s.",
			details.ServiceName, details.Schedule, config.Schedule, next.Format(time.RFC3339))
		task.Set(scheduleDetailsAttr, &scheduleDetails{
			ServiceName: details.ServiceName,
			Schedule:    config.Schedule,
			Next:        next,
		})
		shouldEnsure = true
	}

	// Start tracking schedules for services that are newly configured with
	// one (and don't already have a change tracking them).
	names := make([]string, 0, len(newPlan.Services))
	for name := range newPlan.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		config := newPlan.Services[name]
		if existing[name] || config.Schedule == "" {
			continue
		}
		// Use "yesterday" as the reference point so that a schedule window
		// that's already open today is picked up immediately, without
		// having timeutil step forward one day at a time from long ago.
		next, err := nextScheduleTime(config.Schedule, timeNow().Add(-24*time.Hour))
		if err != nil {
			logger.Noticef("Cannot parse schedule %q for service %q: %v", config.Schedule, name, err)
			continue
		}
		serviceScheduleChange(m.state, name, config.Schedule, next)
		shouldEnsure = true
	}

	if shouldEnsure {
		m.state.EnsureBefore(0)
	}
}

// serviceIsActive reports whether the named service is currently started,
// starting, or otherwise considered "running" for scheduling purposes.
func (m *ServiceManager) serviceIsActive(name string) bool {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	s := m.services[name]
	if s == nil {
		return false
	}
	switch s.state {
	case stateInitial, stateStarting, stateRunning:
		return true
	default:
		return false
	}
}

// startServiceOnSchedule creates an independent "start" change for name (the
// same machinery used by the API's start/replan actions), and returns its
// change ID. The caller must hold the state lock.
func (m *ServiceManager) startServiceOnSchedule(name string) (changeID string, err error) {
	lanes, err := m.StartOrder([]string{name})
	if err != nil {
		return "", err
	}
	taskSet, err := Start(m.state, lanes)
	if err != nil {
		return "", err
	}
	change := m.state.NewChange("start", fmt.Sprintf("Start service %q on schedule", name))
	change.AddAll(taskSet)
	m.state.EnsureBefore(0)
	return change.ID(), nil
}

// ensureSchedules starts any services whose scheduled timer has elapsed, and to
// reschedule the next start.
func (m *ServiceManager) ensureSchedules() error {
	m.state.Lock()
	defer m.state.Unlock()

	var (
		now    time.Time     = timeNow()
		before time.Duration = math.MaxInt64
	)

	for _, change := range m.state.Changes() {
		if change.Kind() != serviceScheduleKind || change.IsReady() {
			continue
		}
		task := change.Tasks()[0]
		if !task.Has(scheduleDetailsAttr) {
			continue
		}

		var details scheduleDetails
		err := task.Get(scheduleDetailsAttr, &details)
		if err != nil {
			return fmt.Errorf("cannot get service-schedule-details from task: %w", err)
		}

		if details.Next.IsZero() {
			continue
		}

		if now.Before(details.Next) {
			before = min(before, details.Next.Sub(now))
			continue
		}

		missed := details.Next
		following, err := nextScheduleTime(details.Schedule, missed)
		if err != nil {
			logger.Noticef("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
			task.Errorf("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
		}

		followingMsg := "not scheduled again"
		if !following.IsZero() {
			followingMsg = fmt.Sprintf("next scheduled start at %s", following.Format(time.RFC3339))
		}
		if scheduleShouldRunNow(now, missed, following) {
			if now.Sub(missed) > scheduleMissThreshold {
				task.Logf("Missed scheduled start at %s for service %q; starting it now.",
					missed.Format(time.RFC3339), details.ServiceName)
			}
			if m.serviceIsActive(details.ServiceName) {
				task.Logf("Service %q is already running; %s.", details.ServiceName, followingMsg)
			} else {
				startedChangeID, err := m.startServiceOnSchedule(details.ServiceName)
				if err != nil {
					task.Errorf("Cannot start service %q on schedule: %v", details.ServiceName, err)
				} else {
					task.Logf("Started service %q on schedule (change %q); %s.",
						details.ServiceName, startedChangeID, followingMsg)
				}
			}
		} else {
			task.Logf("Skipped scheduled start at %s for service %q (missed by too long); %s.",
				missed.Format(time.RFC3339), details.ServiceName, followingMsg)
		}

		details.Next = following
		task.Set(scheduleDetailsAttr, &details)
		before = min(before, following.Sub(now))
	}

	if before < math.MaxInt64 {
		m.state.EnsureBefore(before)
	}

	return nil
}
