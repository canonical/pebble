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

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

func (m *CheckManager) doPerformCheck(task *state.Task, tomb *tomb.Tomb) error {
	var details checkDetails
	m.state.Lock()
	changeID := task.Change().ID()
	err := task.Get("check-details", &details)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for task %q: %v", task.ID(), err)
	}

	m.planLock.Lock()
	plan := m.plan
	m.planLock.Unlock()
	config, ok := plan.Checks[details.Name]
	if !ok {
		logger.Debugf("Check %q no longer exists in plan.", details.Name)
		return nil
	}

	m.checksLock.Lock()
	data := m.checks[config.Name]
	if data == nil {
		data = &checkData{
			config:   config,
			cancel:   func() { tomb.Kill(nil) },
			changeID: changeID,
		}
		m.checks[config.Name] = data
	}
	m.checksLock.Unlock()

	logger.Debugf("Performing check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config, plan)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), config, chk, config.Timeout.Value)
			if err != nil {
				// Check failed, switch to recovering a failing check.
				failures, actionRan := m.handleCheckFailure(config, err)

				// Hold checksLock while updating state so that if someone
				// calls Checks() they always see the latest state. We must
				// always acquire checksLock and then state lock in that order.

				m.checksLock.Lock()

				m.state.Lock()
				newChangeID := m.recoverCheckChange(config)
				m.state.EnsureBefore(0)
				m.state.Unlock()

				data.failures = failures
				data.actionRan = actionRan
				data.changeID = newChangeID
				m.checksLock.Unlock()

				// Returning the error means perform-check goes to Error status
				// and logs the error to the task log.
				return err
			}

		case <-tomb.Dying():
			logger.Debugf("Check %q stopped during perform-check: %v", config.Name, tomb.Err())
			return tomb.Err()
		}
	}
}

func runCheck(ctx context.Context, config *plan.Check, chk checker, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := chk.check(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			// Check was stopped, don't trigger failure action.
			logger.Debugf("Check %q canceled in flight", config.Name)
			return nil
		}
		return err
	}
	return nil
}

func (m *CheckManager) doRecoverCheck(task *state.Task, tomb *tomb.Tomb) error {
	var details checkDetails
	m.state.Lock()
	changeID := task.Change().ID()
	err := task.Get("check-details", &details)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get check details for task %q: %v", task.ID(), err)
	}

	m.planLock.Lock()
	plan := m.plan
	m.planLock.Unlock()
	config, ok := plan.Checks[details.Name]
	if !ok {
		logger.Debugf("Check %q no longer exists in plan.", details.Name)
		return nil
	}

	m.checksLock.Lock()
	data := m.checks[config.Name]
	if data == nil {
		data = &checkData{
			config:   config,
			cancel:   func() { tomb.Kill(nil) },
			failures: 1,
			changeID: changeID,
		}
		m.checks[config.Name] = data
	}
	m.checksLock.Unlock()

	logger.Debugf("Recovering check %q with period %v", details.Name, config.Period.Value)
	ticker := time.NewTicker(config.Period.Value)
	defer ticker.Stop()

	chk := newChecker(config, plan)
	for {
		select {
		case <-ticker.C:
			err := runCheck(tomb.Context(nil), config, chk, config.Timeout.Value)
			if err != nil {
				failures, actionRan := m.handleCheckFailure(config, err)

				m.checksLock.Lock()

				m.state.Lock()
				task.Errorf("%v", err) // add error to task log
				m.state.Unlock()

				data.failures = failures
				data.actionRan = actionRan
				m.checksLock.Unlock()
				break
			}

			// Check succeeded, switch to performing a succeeding check.
			m.checksLock.Lock()

			m.state.Lock()
			newChangeID := m.performCheckChange(config)
			m.state.EnsureBefore(0)
			m.state.Unlock()

			data.failures = 0
			data.actionRan = false
			data.changeID = newChangeID
			m.checksLock.Unlock()
			return nil

		case <-tomb.Dying():
			logger.Debugf("Check %q stopped during recover-check: %v", config.Name, tomb.Err())
			return tomb.Err()
		}
	}
}

func (m *CheckManager) handleCheckFailure(config *plan.Check, err error) (failures int, actionRan bool) {
	m.checksLock.Lock()
	data := m.checks[config.Name]
	failures = data.failures
	actionRan = data.actionRan
	m.checksLock.Unlock()

	failures++
	logger.Noticef("Check %q failure %d/%d: %v", config.Name, failures, config.Threshold, err)
	if !actionRan && failures >= config.Threshold {
		logger.Noticef("Check %q threshold %d hit, triggering action", config.Name, config.Threshold)
		m.callFailureHandlers(config.Name)
		actionRan = true
	}
	return failures, actionRan
}
