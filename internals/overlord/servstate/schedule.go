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

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/timeutil"
)

const (
	// serviceScheduleKind is the kind used for the long-lived change that
	// tracks a service's scheduled starts. There is at most one such change
	// per service at any given time, and it persists for as long as the
	// service has a schedule configured: it's never recreated, only ever
	// added to (and trimmed).
	//
	// The change holds a sequence of "start" tasks (the same kind used for
	// manually-requested starts), one per occurrence of the schedule. All
	// but (at most) one of them are Ready (Done or Error), forming a bounded
	// history (see maxScheduleHistory); the remaining one is still pending
	// (DoStatus) with its At time set to the next occurrence, so the task
	// runner starts it automatically once that time arrives.
	//
	// When a pending task's time arrives and it's processed (see
	// prepareScheduledStart, called from doStart), a new pending task
	// tracking the following occurrence is added to the same change before
	// the current one is allowed to finish, so the change never becomes
	// ready while the service still has a schedule. The only time the
	// change does become ready is when the service's schedule is removed
	// (see scheduleChanged), at which point it's marked Done.
	serviceScheduleKind = "service-schedule"

	// scheduleDetailsAttr is the task attribute holding scheduleDetails. It's
	// set on every task created to track a scheduled occurrence (see
	// newScheduledStartTask), and is what distinguishes those tasks from
	// ordinary "start" tasks created by other means (for example, a manual
	// start request).
	scheduleDetailsAttr = "service-schedule-details"

	// scheduleNoPruneAttr marks the schedule change as one that must never
	// be aborted while it's still tracking a service's schedule (it has no
	// natural "abandoned" state, since it's expected to stay unready
	// indefinitely).
	scheduleNoPruneAttr = "service-schedule-no-prune"

	// maxScheduleLookahead bounds how far in the future we search for the next
	// scheduled start time.
	maxScheduleLookahead = 366 * 24 * time.Hour

	// scheduleMissThreshold is how overdue a scheduled start has to be before
	// we call it out explicitly as "missed" in the task log.
	scheduleMissThreshold = 5 * time.Second

	// maxScheduleHistory is the maximum number of finished (Done or Error)
	// tasks kept as history in a service-schedule change. Once a new
	// occurrence is scheduled, the oldest finished task is removed if the
	// change already holds this many.
	maxScheduleHistory = 5
)

// scheduleDetails is persisted on every task tracking a scheduled occurrence,
// and records the schedule string, along with the time the service should be
// (or was) started for that occurrence.
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

// newScheduledStartTask creates (but doesn't add to any change) a "start"
// task tracking a service's scheduled occurrence at next. It's exactly like
// a task created by Start, except it also carries scheduleDetailsAttr, which
// is what identifies it (to prepareScheduledStart and scheduleChanged) as
// tracking a scheduled occurrence rather than a manually-requested start.
func newScheduledStartTask(st *state.State, name, scheduleStr string, next time.Time) *state.Task {
	task := st.NewTask("start", fmt.Sprintf("Start service %q", name))
	task.Set("service-request", &ServiceRequest{Name: name})
	task.Set(scheduleDetailsAttr, &scheduleDetails{
		ServiceName: name,
		Schedule:    scheduleStr,
		Next:        next,
	})
	task.At(next)
	return task
}

// serviceScheduleChange creates the long-lived change used to track name's
// scheduled starts, with a single pending task tracking the occurrence at
// next, and returns the change ID.
// The caller must hold the state lock.
func serviceScheduleChange(st *state.State, name, scheduleStr string, next time.Time) string {
	task := newScheduledStartTask(st, name, scheduleStr, next)

	changeSummary := fmt.Sprintf("Schedule service %q", name)
	change := st.NewChange(serviceScheduleKind, changeSummary)
	change.Set(scheduleNoPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

// pendingScheduleTask returns the task in a service-schedule change that's
// still waiting for its scheduled time to arrive, i.e. the one tracking the
// service's next scheduled occurrence, or nil if there is currently none.
//
// The latter is normally only the case right after the service's schedule
// has been removed (see scheduleChanged); while a scheduled occurrence is
// being processed (see prepareScheduledStart), the task for the following
// occurrence is added before the current one finishes, so there's a brief
// window with two unready tasks in the change (the one being processed, and
// the newly-added pending one) rather than none.
func pendingScheduleTask(chg *state.Change) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Status() == state.DoStatus && t.Has(scheduleDetailsAttr) {
			return t
		}
	}
	return nil
}

// scheduleHasPendingTask reports whether chg already has a task (other than
// exclude) that isn't Ready yet.
func scheduleHasPendingTask(chg *state.Change, exclude *state.Task) bool {
	for _, t := range chg.Tasks() {
		if t.ID() == exclude.ID() {
			continue
		}
		if !t.Status().Ready() {
			return true
		}
	}
	return false
}

// pruneScheduleHistory removes the oldest finished (Done or Error) tasks
// from a service-schedule change, keeping at most maxScheduleHistory of
// them. reserve accounts for additional tasks (not yet Ready) that are
// guaranteed to become finished imminently but aren't Ready yet at the
// time of this call -- the task currently being processed by
// prepareScheduledStart, still Doing at this point, is one such task.
// The caller must hold the state lock.
func pruneScheduleHistory(chg *state.Change, reserve int) {
	tasks := chg.Tasks()
	finished := make([]*state.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Status().Ready() {
			finished = append(finished, t)
		}
	}
	excess := len(finished) + reserve - maxScheduleHistory
	for i := range excess {
		chg.RemoveTask(finished[i])
	}
}

// prepareScheduledStart is called from doStart, while holding the state
// lock, for every "start" task. If task isn't tracking a scheduled
// occurrence (i.e. wasn't created by newScheduledStartTask), it's a no-op.
//
// Otherwise, it queues up a task to track the service's following scheduled
// occurrence in the same change (pruning old history in the process, and
// guarding against doing so twice if Pebble restarted partway through
// handling this occurrence), then decides whether the actual start should
// proceed now, logging the outcome either way. It returns skip=true if the
// caller should not go on to actually start the service (either because the
// scheduled start was missed by too long, or because the service is already
// running).
func (m *ServiceManager) prepareScheduledStart(task *state.Task) (skip bool, err error) {
	if !task.Has(scheduleDetailsAttr) {
		return false, nil
	}
	var details scheduleDetails
	if err := task.Get(scheduleDetailsAttr, &details); err != nil {
		return false, fmt.Errorf("cannot get service-schedule-details from task: %w", err)
	}

	now := timeNow()
	missed := details.Next
	following, err := nextScheduleTimeAfter(details.Schedule, missed)
	if err != nil {
		logger.Noticef("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
		task.Errorf("Cannot compute next scheduled start for service %q: %v", details.ServiceName, err)
	}

	// Queue up the following occurrence in the same change (pruning old
	// history if needed) before deciding what to do about the occurrence
	// that's due now, so the change never becomes ready while there's still
	// a schedule to track. The pending-task check guards against doing this
	// twice for the same occurrence, in case Pebble restarted between
	// queuing the next occurrence and finishing this task.
	if chg := task.Change(); chg != nil && !following.IsZero() && !scheduleHasPendingTask(chg, task) {
		next := newScheduledStartTask(m.state, details.ServiceName, details.Schedule, following)
		chg.AddTask(next)
		pruneScheduleHistory(chg, 1)
		m.state.EnsureBefore(0)
	}

	followingMsg := "not scheduled again"
	if !following.IsZero() {
		followingMsg = fmt.Sprintf("next scheduled start at %s", following.Format(time.RFC3339))
	}

	if !scheduleShouldRunNow(now, missed, following) {
		task.Logf("Skipped scheduled start at %s for service %q (missed by too long); %s.",
			missed.Format(time.RFC3339), details.ServiceName, followingMsg)
		return true, nil
	}

	if now.Sub(missed) > scheduleMissThreshold {
		task.Logf("Missed scheduled start at %s for service %q; starting it now.",
			missed.Format(time.RFC3339), details.ServiceName)
	}
	if m.serviceIsActive(details.ServiceName) {
		task.Logf("Service %q is already running; %s.", details.ServiceName, followingMsg)
		return true, nil
	}

	return false, nil
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
		task := pendingScheduleTask(change)
		if task == nil {
			// No task currently waiting on its scheduled time (for example,
			// a scheduled start is being processed right now): nothing to
			// update here; it'll be picked up on a following PlanChanged.
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
			// mark the pending task (and so the change) Done directly
			// (rather than via change.Abort(), which would go through the
			// Abort/Undo dance) since there's nothing to undo here.
			reason := "its schedule was removed from the plan"
			if !inPlan {
				reason = "it was removed from the plan"
			}
			task.Logf("Service %q unscheduled: %s; no more scheduled starts.", details.ServiceName, reason)
			task.SetStatus(state.DoneStatus)
			change.SetStatus(state.DoneStatus)
			shouldEnsure = true
			continue
		}

		if config.Schedule == details.Schedule {
			// Schedule hasn't changed.
			continue
		}

		// Schedule string changed: recompute the next scheduled start time, but
		// reuse the existing task rather than creating a new one.
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
// service that currently has a task waiting on its scheduled time.
//
// The caller must hold the state lock.
func (m *ServiceManager) scheduledStartTimes() map[string]time.Time {
	scheduled := make(map[string]time.Time)
	for _, change := range m.state.Changes() {
		if change.Kind() != serviceScheduleKind {
			continue
		}
		task := pendingScheduleTask(change)
		if task == nil {
			continue
		}
		var details scheduleDetails
		if err := task.Get(scheduleDetailsAttr, &details); err != nil {
			continue
		}
		scheduled[details.ServiceName] = details.Next
	}
	return scheduled
}
