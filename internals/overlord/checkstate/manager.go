// Copyright (c) 2021 Canonical Ltd
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

// TODO: should it only go to recover-check when it hits the threshold?

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

const (
	noPruneAttr = "checkstate-no-prune"
)

// CheckManager starts and manages the health checks.
type CheckManager struct {
	state      *state.State
	ensureDone atomic.Bool

	checksLock sync.Mutex
	checks     map[string]*checkData

	failureHandlers []FailureFunc

	planLock sync.Mutex
	plan     *plan.Plan
}

// FailureFunc is the type of function called when a failure action is triggered.
type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager(s *state.State, runner *state.TaskRunner) *CheckManager {
	manager := &CheckManager{
		state:  s,
		checks: make(map[string]*checkData),
	}
	// Health check changes can be long-running; ensure they don't get pruned.
	s.RegisterPendingChangeByAttr(noPruneAttr, func(change *state.Change) bool {
		return true
	})
	runner.AddHandler("perform-check", manager.doPerformCheck, nil)
	runner.AddHandler("recover-check", manager.doRecoverCheck, nil)
	return manager
}

func (m *CheckManager) Ensure() error {
	m.ensureDone.Store(true)
	return nil
}

func (c *CheckManager) Stop() {
	// TODO: stop/cancel running checks
}

// NotifyCheckFailed adds f to the list of functions that are called whenever
// a check hits its failure threshold.
func (m *CheckManager) NotifyCheckFailed(f FailureFunc) {
	m.failureHandlers = append(m.failureHandlers, f)
}

// PlanChanged handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *CheckManager) PlanChanged(newPlan *plan.Plan) {
	// TODO: should figure out how to not stop/restart checks that haven't changed
	//       so that unnecessary changes/tasks aren't created

	// First cancel tasks of currently-running checks.
	m.checksLock.Lock()
	for name, data := range m.checks {
		if data.cancel != nil {
			data.cancel()
		}
		delete(m.checks, name)
	}
	m.checksLock.Unlock()

	// Update local reference to plan.
	m.planLock.Lock()
	m.plan = newPlan
	m.planLock.Unlock()

	// Start updated checks.
	m.state.Lock()
	defer m.state.Unlock()
	for _, config := range newPlan.Checks {
		m.performCheckChange(config)
	}
	if !m.ensureDone.Load() {
		// Can't call EnsureBefore before Overlord.Loop is running (which will
		// call m.Ensure for the first time).
		return
	}
	m.state.EnsureBefore(0) // start new tasks right away
}

func (m *CheckManager) performCheckChange(config *plan.Check) (changeID string) {
	task := performCheck(m.state, config.Name, checkType(config))
	change := m.state.NewChange("perform-check", task.Summary())
	change.Set(noPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

func (m *CheckManager) recoverCheckChange(config *plan.Check) (changeID string) {
	task := recoverCheck(m.state, config.Name, checkType(config))
	change := m.state.NewChange("recover-check", task.Summary())
	change.Set(noPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

func (m *CheckManager) callFailureHandlers(name string) {
	for _, f := range m.failureHandlers {
		f(name)
	}
}

// checkType returns a human-readable string representing the check type.
func checkType(config *plan.Check) string {
	switch {
	case config.HTTP != nil:
		return "HTTP"
	case config.TCP != nil:
		return "TCP"
	case config.Exec != nil:
		return "exec"
	default:
		return "<unknown>"
	}
}

// newChecker creates a new checker of the configured type.
func newChecker(config *plan.Check, p *plan.Plan) checker {
	switch {
	case config.HTTP != nil:
		return &httpChecker{
			name:    config.Name,
			url:     config.HTTP.URL,
			headers: config.HTTP.Headers,
		}

	case config.TCP != nil:
		return &tcpChecker{
			name: config.Name,
			host: config.TCP.Host,
			port: config.TCP.Port,
		}

	case config.Exec != nil:
		overrides := plan.ContextOptions{
			Environment: config.Exec.Environment,
			UserID:      config.Exec.UserID,
			User:        config.Exec.User,
			GroupID:     config.Exec.GroupID,
			Group:       config.Exec.Group,
			WorkingDir:  config.Exec.WorkingDir,
		}
		merged, err := plan.MergeServiceContext(p, config.Exec.ServiceContext, overrides)
		if err != nil {
			// Context service name has already been checked when plan was loaded.
			panic("internal error: " + err.Error())
		}
		return &execChecker{
			name:        config.Name,
			command:     config.Exec.Command,
			environment: merged.Environment,
			userID:      merged.UserID,
			user:        merged.User,
			groupID:     merged.GroupID,
			group:       merged.Group,
			workingDir:  merged.WorkingDir,
		}

	default:
		// This has already been checked when parsing the config.
		panic("internal error: invalid check config")
	}
}

// Checks returns the list of currently-configured checks and their status,
// ordered by name.
func (m *CheckManager) Checks() ([]*CheckInfo, error) {
	m.checksLock.Lock()
	infos := make([]*CheckInfo, 0, len(m.checks))
	for _, check := range m.checks {
		infos = append(infos, check.info())
	}
	m.checksLock.Unlock()

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

// CheckInfo provides status information about a single check.
type CheckInfo struct {
	Name      string
	Level     plan.CheckLevel
	Status    CheckStatus
	Failures  int
	Threshold int
	ChangeID  string
}

type CheckStatus string

const (
	CheckStatusUp   CheckStatus = "up"
	CheckStatusDown CheckStatus = "down"
)

// checkData holds state for an active health check.
type checkData struct {
	config    *plan.Check
	cancel    func()
	failures  int
	actionRan bool
	changeID  string
}

type checker interface {
	check(ctx context.Context) error
}

// info returns user-facing check information for use in Checks (and tests).
func (c *checkData) info() *CheckInfo {
	info := &CheckInfo{
		Name:      c.config.Name,
		Level:     c.config.Level,
		Status:    CheckStatusUp,
		Failures:  c.failures,
		Threshold: c.config.Threshold,
		ChangeID:  c.changeID,
	}
	if c.failures >= c.config.Threshold {
		info.Status = CheckStatusDown
	}
	return info
}
