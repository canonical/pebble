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

	// scheduleTaskPollInterval is how often doServiceSchedule checks whether
	// its task has been finished externally.
	scheduleTaskPollInterval = time.Second
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
	summary := fmt.Sprintf("Wait for scheduled start of service %q", name)
	task := st.NewTask(serviceScheduleKind, summary)
	task.Set(scheduleDetailsAttr, &scheduleDetails{
		ServiceName: name,
		Schedule:    scheduleStr,
		Next:        next,
	})
	// doServiceSchedule (registered as this kind's TaskRunner handler) parks
	// until the status below is changed by ensureSchedules/scheduleChanged;
	// it's what drives this task, not the handler itself. Mark it Doing so
	// it's clear from "pebble changes"/"pebble tasks" that it's ongoing.
	task.SetStatus(state.DoingStatus)

	change := st.NewChange(serviceScheduleKind, summary)
	change.Set(scheduleNoPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

// doServiceSchedule is the TaskRunner handler for serviceScheduleKind tasks.
//
// The task's outcome is driven entirely by ServiceManager.Ensure (see
// ensureSchedules) and scheduleChanged, which change its status away from
// DoingStatus once the scheduled time arrives (or the schedule is retired).
// This handler's only job is to occupy the task runner's "do" slot for this
// task kind, so that it isn't picked up by the runner's generic handling for
// tasks with no registered handler (which would otherwise mark it Done
// immediately, defeating the entire point of it representing "still
// waiting"). It just blocks, polling for that external status change, until
// it happens or the runner asks it to stop.
func (m *ServiceManager) doServiceSchedule(task *state.Task, tomb *tomb.Tomb) error {
	for {
		select {
		case <-tomb.Dying():
			return tomb.Err()
		case <-time.After(scheduleTaskPollInterval):
		}
		m.state.Lock()
		stillWaiting := task.Status() == state.DoingStatus
		m.state.Unlock()
		if !stillWaiting {
			return nil
		}
	}
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
		if task.Status() != state.DoingStatus {
			// Already fired: the start task(s) added alongside it are still
			// running. Nothing more to do here until the whole change becomes
			// ready (see scheduleChangeReady).
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
		if scheduleShouldRunNow(now, missed, following) {
			if now.Sub(missed) > scheduleMissThreshold {
				task.Logf("Missed scheduled start at %s for service %q; starting it now.",
					missed.Format(time.RFC3339), details.ServiceName)
			}
			if m.serviceIsActive(details.ServiceName) {
				// Stay on this change/task: just log and move on to the next
				// occurrence.
				task.Logf("Service %q is already running; %s.", details.ServiceName, followingMsg)
			} else {
				lanes, err := m.StartOrder([]string{details.ServiceName})
				if err != nil {
					task.Errorf("Cannot start service %q on schedule: %v", details.ServiceName, err)
				} else {
					taskSet, err := Start(m.state, lanes)
					if err != nil {
						task.Errorf("Cannot start service %q on schedule: %v", details.ServiceName, err)
					} else {
						// Add the start task(s) to this same change, rather
						// than creating an independent one.
						change.AddAll(taskSet)
						task.Logf("Started service %q on schedule; %s.", details.ServiceName, followingMsg)
						fired = true
					}
				}
			}
		} else {
			task.Logf("Skipped scheduled start at %s for service %q (missed by too long); %s.",
				missed.Format(time.RFC3339), details.ServiceName, followingMsg)
		}

		// Record the following occurrence, whether we fired or not: if we
		// fired, scheduleChangeReady reads this back once the change becomes
		// ready, to create the follow-up change.
		details.Next = following
		task.Set(scheduleDetailsAttr, &details)

		if fired {
			// Mark the service-schedule task done now that the start task(s)
			// have been added to the change (must happen after AddAll/Set
			// above, so the change's ready channel doesn't close
			// prematurely). The change becomes ready once the start task(s)
			// finish, at which point scheduleChangeReady takes over.
			task.SetStatus(state.DoneStatus)
		} else {
			before = min(before, following.Sub(now))
		}
	}

	if before < math.MaxInt64 {
		m.state.EnsureBefore(before)
	}

	return nil
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
