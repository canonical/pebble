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

	m.checksLock.Lock()
	data := m.ensureCheck(config.Name)
	refresh := data.refresh
	m.checksLock.Unlock()

	chk := newChecker(config)

	performCheck := func() (shouldExit bool, err error) {
		//lint:ignore SA1012 providing a nil context to tomb.Context() is valid
		err = runCheck(tomb.Context(nil), chk, config.Timeout.Value)
		if !tomb.Alive() {
			return true, checkStopped(config.Name, task.Kind(), tomb.Err())
		}
		if err != nil {
			m.incFailureMetric(config)
			// Record check failure and perform any action if the threshold
			// is reached (for example, restarting a service).
			details.Failures++
			m.updateCheckData(config, changeID, details.Successes, details.Failures)

			m.state.Lock()
			atThreshold := details.Failures >= config.Threshold
			if atThreshold {
				details.Proceed = true
			} else {
				// Add error to task log, but only if we haven't reached the
				// threshold. When we hit the threshold, the "return err"
				// below will cause the error to be logged.
				task.Errorf("%s", errorDetails(err))
			}
			task.Set(checkDetailsAttr, &details)
			m.state.Unlock()

			logger.Noticef("Check %q failure %d/%d: %v", config.Name, details.Failures, config.Threshold, err)
			if atThreshold {
				logger.Noticef("Check %q threshold %d hit, triggering action and recovering", config.Name, config.Threshold)
				m.callFailureHandlers(config.Name)
				// Returning the error means perform-check goes to Error status
				// and logs the error to the task log.
				return true, err
			}
			return false, err
		}

		m.incSuccessMetric(config)
		details.Successes++
		if details.Failures > 0 {
			oldFailures := details.Failures
			details.Failures = 0
			m.updateCheckData(config, changeID, details.Successes, details.Failures)

			m.state.Lock()
			task.Logf("succeeded after %s", pluralise(oldFailures, "failure", "failures"))
			task.Set(checkDetailsAttr, &details)
			m.state.Unlock()
		} else {
			m.updateCheckData(config, changeID, details.Successes, details.Failures)

			m.state.Lock()
			task.Set(checkDetailsAttr, &details)
			m.state.Unlock()
		}
		return false, nil
	}

	for {
		select {
		case info := <-refresh:
			// Reset ticker on refresh.
			ticker.Reset(config.Period.Value)
			shouldExit, err := performCheck()
			select {
			case info.result <- err:
			case <-info.ctx.Done():
			}
			if shouldExit {
				return err
			}
		case <-ticker.C:
			shouldExit, err := performCheck()
			if shouldExit {
				return err
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

	m.checksLock.Lock()
	data := m.ensureCheck(config.Name)
	refresh := data.refresh
	m.checksLock.Unlock()

	chk := newChecker(config)

	recoverCheck := func() (shouldExit bool, err error) {
		//lint:ignore SA1012 providing a nil context to tomb.Context() is valid
		err = runCheck(tomb.Context(nil), chk, config.Timeout.Value)
		if !tomb.Alive() {
			return true, checkStopped(config.Name, task.Kind(), tomb.Err())
		}
		if err != nil {
			m.incFailureMetric(config)
			details.Failures++
			m.updateCheckData(config, changeID, details.Successes, details.Failures)

			m.state.Lock()
			task.Set(checkDetailsAttr, &details)
			task.Errorf("%s", errorDetails(err))
			m.state.Unlock()

			logger.Noticef("Check %q failure %d/%d: %v", config.Name, details.Failures, config.Threshold, err)
			return false, err
		}

		// Check succeeded, switch to performing a succeeding check.
		m.incSuccessMetric(config)
		details.Successes = 1
		details.Failures = 0
		m.updateCheckData(config, changeID, details.Successes, details.Failures)
		details.Proceed = true
		m.state.Lock()
		task.Set(checkDetailsAttr, &details)
		m.state.Unlock()
		return true, nil
	}

	for {
		select {
		case info := <-refresh:
			// Reset ticker on refresh.
			ticker.Reset(config.Period.Value)
			shouldExit, err := recoverCheck()
			select {
			case info.result <- err:
			case <-info.ctx.Done():
			}
			if shouldExit {
				return err
			}
		case <-ticker.C:
			shouldExit, err := recoverCheck()
			if shouldExit {
				return err
			}
		case <-tomb.Dying():
			return checkStopped(config.Name, task.Kind(), tomb.Err())
		}
	}
}

func errorDetails(err error) string {
	message := err.Error()
	var detailsErr *detailsError
	if errors.As(err, &detailsErr) && detailsErr.Details() != "" {
		message += "; " + detailsErr.Details()
	}
	return message
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
