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
	"sort"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/timeutil"
)

const (
	// serviceScheduleKind is the kind used for both the change and the first
	// task in it, which tracks a service's next scheduled start time. There
	// is at most one such change per service at any given time.
	//
	// The change starts out with just this one task, in DoingStatus, while
	// waiting for the scheduled time. Once that time arrives and the service
	// is actually started, a "start" task (or task set, if the service has
	// dependencies) is added to the same change and the service-schedule
	// task is marked Done. Once the start task(s) finish, the whole change
	// becomes ready, and scheduleChangeReady creates a new service-schedule
	// change to track the next occurrence.
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

// nextScheduleTimeAfter is like nextScheduleTime, but guarantees that the
// result (if non-zero) is strictly after the current time.
//
// This matters because timeutil.Next can return a time that isn't after now
// even when "last" is itself a past occurrence: for a schedule with a spread
// (randomised) window, re-evaluating from a previous occurrence can land back
// inside the same still-open window with a new random offset. Without this
// guard, that can result in a scheduled start's "next" time never actually
// advancing into the future, causing it to be treated as immediately due
// again and again.
//
// Re-deriving from the current time instead forces progress, since "now" is
// always considered part of whatever window contains it, so that window gets
// skipped in favour of a later one.
func nextScheduleTimeAfter(scheduleStr string, last time.Time) (time.Time, error) {
	next, err := nextScheduleTime(scheduleStr, last)
	if err != nil {
		return time.Time{}, err
	}
	now := timeNow()
	if !next.IsZero() && !next.After(now) {
		next, err = nextScheduleTime(scheduleStr, now)
		if err != nil {
			return time.Time{}, err
		}
	}
	return next, nil
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
	taskSummary := fmt.Sprintf("Wait for scheduled start of service %q", name)
	task := st.NewTask(serviceScheduleKind, taskSummary)
	task.Set(scheduleDetailsAttr, &scheduleDetails{
		ServiceName: name,
		Schedule:    scheduleStr,
		Next:        next,
	})
	task.SetStatus(state.DoingStatus)
	task.At(next)

	changeSummary := fmt.Sprintf("Scheduled start of service %q", name)
	change := st.NewChange(serviceScheduleKind, changeSummary)
	change.Set(scheduleNoPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

// doServiceSchedule is the TaskRunner handler for serviceScheduleKind tasks.
// The task runner invokes it once the task's scheduled time arrives (set via
// task.At, see serviceScheduleChange and scheduleChanged).
//
// It decides whether to start the service now (adding a "start" task set to
// the same change, after which the task runner marks this task Done once we
// return) or to keep waiting for the next occurrence (rescheduling ourselves
// and return Retry, so the task runner invokes us again then).
func (m *ServiceManager) doServiceSchedule(task *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var details scheduleDetails
	err := task.Get(scheduleDetailsAttr, &details)
	if err != nil {
		return fmt.Errorf("cannot get service-schedule-details from task: %w", err)
	}

	now := timeNow()
	if now.Before(details.Next) {
		// Woken up early, go back to sleep.
		task.At(details.Next)
		return &state.Retry{}
	}

	missed := details.Next
	following, err := nextScheduleTimeAfter(details.Schedule, missed)
	if err != nil {
		logger.Noticef("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
		task.Errorf("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
	}

	followingMsg := "not scheduled again"
	if !following.IsZero() {
		followingMsg = fmt.Sprintf("next scheduled start at %s", following.Format(time.RFC3339))
	}

	fired := false
	if !scheduleShouldRunNow(now, missed, following) {
		task.Logf("Skipped scheduled start at %s for service %q (missed by too long); %s.",
			missed.Format(time.RFC3339), details.ServiceName, followingMsg)
	} else {
		if now.Sub(missed) > scheduleMissThreshold {
			task.Logf("Missed scheduled start at %s for service %q; starting it now.",
				missed.Format(time.RFC3339), details.ServiceName)
		}
		if m.serviceIsActive(details.ServiceName) {
			task.Logf("Service %q is already running; %s.", details.ServiceName, followingMsg)
		} else if lanes, err := m.StartOrder([]string{details.ServiceName}); err != nil {
			task.Errorf("Cannot start service %q on schedule: %v", details.ServiceName, err)
		} else if taskSet, err := Start(m.state, lanes); err != nil {
			task.Errorf("Cannot start service %q on schedule: %v", details.ServiceName, err)
		} else {
			task.Change().AddAll(taskSet)
			task.Logf("Started service %q on schedule; %s.", details.ServiceName, followingMsg)
			fired = true
		}
	}

	// Record the following occurrence, whether we fired or not: if we fired,
	// scheduleChangeReady reads this back once the change becomes ready, to
	// create the following change.
	details.Next = following
	task.Set(scheduleDetailsAttr, &details)

	if fired {
		m.state.EnsureBefore(0)
		return nil
	}

	// Reschedule ourselves for the next occurrence and ask the task runner to
	// start this task again then.
	task.At(following)
	return &state.Retry{}
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
		// The first task is always the service-schedule task. It may already
		// be Done at this point (with a "start" task set alongside it in the
		// same change, still running) if the scheduled time has already
		// passed; that's fine, we still want to update/retire it below.
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
			// retire this change directly (rather than via change.Abort(),
			// which would go through the Abort/Undo dance) since there's
			// nothing to undo here.
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
		next, err := nextScheduleTimeAfter(config.Schedule, details.Next)
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
		// If the task hasn't fired yet, reschedule it.
		task.At(next)
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

// scheduledStartTimes returns the next scheduled start time for every
// service that currently has a service-schedule task in the Doing status,
// i.e. is waiting for its next scheduled start.
//
// The caller must hold the state lock.
func (m *ServiceManager) scheduledStartTimes() map[string]time.Time {
	scheduled := make(map[string]time.Time)
	for _, change := range m.state.Changes() {
		if change.Kind() != serviceScheduleKind {
			continue
		}
		for _, task := range change.Tasks() {
			if task.Kind() != serviceScheduleKind || task.Status() != state.DoingStatus {
				continue
			}
			var details scheduleDetails
			if err := task.Get(scheduleDetailsAttr, &details); err != nil {
				continue
			}
			scheduled[details.ServiceName] = details.Next
		}
	}
	return scheduled
}

// scheduleChangeReady is called whenever a change's status changes; it looks
// for service-schedule changes that have just become ready (i.e. their
// service-schedule task fired and any start task(s) added alongside it have
// now finished), and creates a new service-schedule change to track the
// service's next scheduled occurrence.
//
// The caller must hold the state lock; this is guaranteed by the state
// package for change-status-changed handlers.
func (m *ServiceManager) scheduleChangeReady(chg *state.Change, old, new state.Status) {
	if chg.Kind() != serviceScheduleKind || old.Ready() || !new.Ready() {
		return
	}

	tasks := chg.Tasks()
	if len(tasks) == 0 {
		return
	}
	// The first task is always the service-schedule task.
	task := tasks[0]
	if task.Status() != state.DoneStatus {
		// The change became ready for some other reason (for example, it was
		// retired via HoldStatus because the service or its schedule was
		// removed from the plan): don't schedule a follow-up.
		return
	}

	var details scheduleDetails
	err := task.Get(scheduleDetailsAttr, &details)
	if err != nil {
		logger.Noticef("Cannot get %s change %s schedule details: %v", chg.Kind(), chg.ID(), err)
		return
	}
	if details.Next.IsZero() {
		// No further occurrence (schedule string yielded nothing within the
		// lookahead window, or failed to parse).
		return
	}

	serviceScheduleChange(m.state, details.ServiceName, details.Schedule, details.Next)
	m.state.EnsureBefore(0)
}
