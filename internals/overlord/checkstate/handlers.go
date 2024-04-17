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
)

func (m *CheckManager) doPerformCheck(task *state.Task, tomb *tombpkg.Tomb) error {
	var details checkDetails

	m.state.Lock() // always acquire locks in same order (state lock, then plan lock)
	m.planLock.Lock()
	plan := m.plan
	m.planLock.Unlock()
	err := task.Get(checkDetailsAttr, &details)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for perform-check task %q: %v", task.ID(), err)
	}
	config, ok := plan.Checks[details.Name]
	if !ok {
		// Check no longer exists in plan.
		return nil
	}

	logger.Debugf("Performing check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config, plan)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), chk, config.Timeout.Value)
			if err != nil {
				// Record check failure and perform any action if the threshold
				// is reached (for example, restarting a service).
				details.Failures++
				m.state.Lock()
				atThreshold := details.Failures >= config.Threshold
				if atThreshold {
					details.Proceed = true
				} else {
					// Add error to task log, but only if we haven't reached the
					// threshold. When we hit the threshold, the "return err"
					// below will cause the error to be logged.
					task.Errorf("%v", err)
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
			} else {
				// TODO: need a test for this -- this block wasn't here before, and tests passed!
				details.Failures = 0
				m.state.Lock()
				task.Set(checkDetailsAttr, &details)
				m.state.Unlock()
			}

		case <-tomb.Dying():
			break
		}

		// Do this check here so that select doesn't sometimes pick up another
		// ticker.C tick after the tomb has been killed.
		tombErr := tomb.Err()
		if tombErr != tombpkg.ErrStillAlive {
			logger.Debugf("Check %q stopped during perform-check%s", config.Name, errReason(tombErr))
			return tombErr
		}
	}
}

func runCheck(ctx context.Context, chk checker, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := chk.check(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			// Check was stopped, don't trigger failure action (tomb.Err check
			// in caller will return from the handler).
			return nil
		}
		return err
	}
	return nil
}

func (m *CheckManager) doRecoverCheck(task *state.Task, tomb *tombpkg.Tomb) error {
	var details checkDetails

	m.state.Lock() // always acquire locks in same order (state lock, then plan lock)
	m.planLock.Lock()
	plan := m.plan
	m.planLock.Unlock()
	err := task.Get(checkDetailsAttr, &details)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for recover-check task %q: %v", task.ID(), err)
	}
	config, ok := plan.Checks[details.Name]
	if !ok {
		// Check no longer exists in plan.
		return nil
	}

	logger.Debugf("Recovering check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config, plan)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), chk, config.Timeout.Value)
			if err != nil {
				details.Failures++
				m.state.Lock()
				task.Set(checkDetailsAttr, &details)
				task.Errorf("%v", err) // add error to task log
				m.state.Unlock()

				logger.Noticef("Check %q failure %d/%d: %v", config.Name, details.Failures, config.Threshold, err)
				break
			}

			// Check succeeded, switch to performing a succeeding check.
			details.Proceed = true
			m.state.Lock()
			task.Set(checkDetailsAttr, &details)
			m.state.Unlock()
			return nil

		case <-tomb.Dying():
			break
		}

		// Do this check here so that select doesn't sometimes pick up another
		// ticker.C tick after the tomb has been killed.
		tombErr := tomb.Err()
		if tombErr != tombpkg.ErrStillAlive {
			logger.Debugf("Check %q stopped during recover-check%s", config.Name, errReason(tombErr))
			return tombErr
		}
	}
}

func errReason(err error) string {
	if err == nil {
		return " (no error)"
	}
	return ": " + err.Error()
}
