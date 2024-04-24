// Copyright (c) 2024 Canonical Ltd
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

package checkstate

import (
	"context"
	"errors"
	"fmt"
	"time"

	tombpkg "gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

func (m *CheckManager) doPerformCheck(task *state.Task, tomb *tombpkg.Tomb) error {
	m.state.Lock()
	changeID := task.Change().ID()
	var details checkDetails
	err := task.Get(checkDetailsAttr, &details)
	config := m.state.Cached(performConfigKey{changeID}).(*plan.Check) // panic if key not present (always should be)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for perform-check task %q: %v", task.ID(), err)
	}

	logger.Debugf("Performing check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), chk, config.Timeout.Value)
			if !tomb.Alive() {
				return checkStopped(config.Name, task.Kind(), tomb.Err())
			}
			if err != nil {
				// Record check failure and perform any action if the threshold
				// is reached (for example, restarting a service).
				details.Failures++
				atThreshold := details.Failures >= config.Threshold
				if !atThreshold {
					// Update number of failures in check info. In threshold
					// case, check info will be updated with new change ID by
					// changeStatusChanged.
					m.updateCheckInfo(config, changeID, details.Failures)
				}

				m.state.Lock()
				if atThreshold {
					details.Proceed = true
				} else {
					// Add error to task log, but only if we haven't reached the
					// threshold. When we hit the threshold, the "return err"
					// below will cause the error to be logged.
					logTaskError(task, err)
				}
				task.Set(checkDetailsAttr, &details)
				m.state.Unlock()

				logger.Noticef("Check %q failure %d/%d: %v", config.Name, details.Failures, config.Threshold, err)
				if atThreshold {
					logger.Noticef("Check %q threshold %d hit, triggering action and recovering", config.Name, config.Threshold)
					m.callFailureHandlers(config.Name)
					// Returning the error means perform-check goes to Error status
					// and logs the error to the task log.
					return err
				}
			} else if details.Failures > 0 {
				m.updateCheckInfo(config, changeID, 0)

				m.state.Lock()
				task.Logf("succeeded after %s", pluralise(details.Failures, "failure", "failures"))
				details.Failures = 0
				task.Set(checkDetailsAttr, &details)
				m.state.Unlock()
			}

		case <-tomb.Dying():
			return checkStopped(config.Name, task.Kind(), tomb.Err())
		}
	}
}

func runCheck(ctx context.Context, chk checker, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := chk.check(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("check timed out after %v", timeout)
	}
	return err
}

func (m *CheckManager) doRecoverCheck(task *state.Task, tomb *tombpkg.Tomb) error {
	m.state.Lock()
	changeID := task.Change().ID()
	var details checkDetails
	err := task.Get(checkDetailsAttr, &details)
	config := m.state.Cached(recoverConfigKey{changeID}).(*plan.Check) // panic if key not present (always should be)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for recover-check task %q: %v", task.ID(), err)
	}

	logger.Debugf("Recovering check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), chk, config.Timeout.Value)
			if !tomb.Alive() {
				return checkStopped(config.Name, task.Kind(), tomb.Err())
			}
			if err != nil {
				details.Failures++
				m.updateCheckInfo(config, changeID, details.Failures)

				m.state.Lock()
				task.Set(checkDetailsAttr, &details)
				logTaskError(task, err)
				m.state.Unlock()

				logger.Noticef("Check %q failure %d/%d: %v", config.Name, details.Failures, config.Threshold, err)
				break
			}

			// Check succeeded, switch to performing a succeeding check.
			// Check info will be updated with new change ID by changeStatusChanged.
			details.Failures = 0 // not strictly needed, but just to be safe
			details.Proceed = true
			m.state.Lock()
			task.Set(checkDetailsAttr, &details)
			m.state.Unlock()
			return nil

		case <-tomb.Dying():
			return checkStopped(config.Name, task.Kind(), tomb.Err())
		}
	}
}

func logTaskError(task *state.Task, err error) {
	message := err.Error()
	var detailsErr *detailsError
	if errors.As(err, &detailsErr) && detailsErr.Details() != "" {
		message += "; " + detailsErr.Details()
	}
	task.Errorf("%s", message)
}

func checkStopped(checkName, taskKind string, tombErr error) error {
	reason := " (no error)"
	if tombErr != nil {
		reason = ": " + tombErr.Error()
	}
	logger.Debugf("Check %q stopped during %s%s", checkName, taskKind, reason)
	return tombErr
}

func pluralise(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
