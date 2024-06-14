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

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

const (
	performCheckKind = "perform-check"
	recoverCheckKind = "recover-check"

	noPruneAttr      = "check-no-prune"
	checkDetailsAttr = "check-details"
)

// CheckManager starts and manages the health checks.
type CheckManager struct {
	state      *state.State
	ensureDone atomic.Bool

	failureHandlers []FailureFunc

	checksLock sync.Mutex
	checks     map[string]CheckInfo
}

// FailureFunc is the type of function called when a failure action is triggered.
type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager(s *state.State, runner *state.TaskRunner) *CheckManager {
	manager := &CheckManager{
		state:  s,
		checks: make(map[string]CheckInfo),
	}

	// Health check changes can be long-running; ensure they don't get pruned.
	s.RegisterPendingChangeByAttr(noPruneAttr, func(change *state.Change) bool {
		return true
	})

	runner.AddHandler(performCheckKind, manager.doPerformCheck, nil)
	runner.AddHandler(recoverCheckKind, manager.doRecoverCheck, nil)

	runner.AddCleanup(performCheckKind, func(task *state.Task, tomb *tomb.Tomb) error {
		s.Lock()
		defer s.Unlock()
		s.Cache(performConfigKey{task.Change().ID()}, nil)
		return nil
	})
	runner.AddCleanup(recoverCheckKind, func(task *state.Task, tomb *tomb.Tomb) error {
		s.Lock()
		defer s.Unlock()
		s.Cache(recoverConfigKey{task.Change().ID()}, nil)
		return nil
	})

	// Monitor perform-check and recover-check changes for status updates.
	s.Lock()
	s.AddChangeStatusChangedHandler(manager.changeStatusChanged)
	s.Unlock()

	return manager
}

func (m *CheckManager) Ensure() error {
	m.ensureDone.Store(true)
	return nil
}

// NotifyCheckFailed adds f to the list of functions that are called whenever
// a check hits its failure threshold.
func (m *CheckManager) NotifyCheckFailed(f FailureFunc) {
	m.failureHandlers = append(m.failureHandlers, f)
}

// PlanChanged handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *CheckManager) PlanChanged(newPlan *plan.Plan) {
	m.state.Lock()
	defer m.state.Unlock()

	shouldEnsure := false
	newOrModified := make(map[string]bool)
	existingChecks := make(map[string]bool)

	// Abort all currently-running checks that have been removed or modified.
	for _, change := range m.state.Changes() {
		switch change.Kind() {
		case performCheckKind, recoverCheckKind:
			if change.IsReady() {
				// Skip check changes that have finished already.
				continue
			}
			details := mustGetCheckDetails(change)
			var configKey interface{}
			if change.Kind() == performCheckKind {
				configKey = performConfigKey{change.ID()}
			} else {
				configKey = recoverConfigKey{change.ID()}
			}
			v := m.state.Cached(configKey)
			if v == nil {
				// Pebble restarted, and this change is a carryover.
				change.Abort()
				shouldEnsure = true
				continue
			}
			oldConfig := v.(*plan.Check)
			existingChecks[oldConfig.Name] = true

			newConfig, inNew := newPlan.Checks[details.Name]
			if inNew {
				merged := mergeServiceContext(newPlan, newConfig)
				if reflect.DeepEqual(oldConfig, merged) {
					// Don't restart check if its configuration hasn't changed.
					continue
				}
				// Check is in old and new plans and has been modified.
				newOrModified[details.Name] = true
			}
			change.Abort()
			m.deleteCheckInfo(details.Name)
			shouldEnsure = true
		}
	}

	// Also find checks that are new (in new plan but not in old one).
	for _, config := range newPlan.Checks {
		if !existingChecks[config.Name] {
			newOrModified[config.Name] = true
		}
	}

	// Start new or modified checks.
	for _, config := range newPlan.Checks {
		if newOrModified[config.Name] {
			merged := mergeServiceContext(newPlan, config)
			changeID := performCheckChange(m.state, merged)
			m.updateCheckInfo(config, changeID, 0)
			shouldEnsure = true
		}
	}
	if !m.ensureDone.Load() {
		// Can't call EnsureBefore before Overlord.Loop is running (which will
		// call m.Ensure for the first time).
		return
	}
	if shouldEnsure {
		m.state.EnsureBefore(0) // start new tasks right away
	}
}

func (m *CheckManager) changeStatusChanged(change *state.Change, old, new state.Status) {
	shouldEnsure := false
	switch {
	case change.Kind() == performCheckKind && new == state.ErrorStatus:
		details := mustGetCheckDetails(change)
		if !details.Proceed {
			break
		}
		config := m.state.Cached(performConfigKey{change.ID()}).(*plan.Check) // panic if key not present (always should be)
		changeID := recoverCheckChange(m.state, config, details.Failures)
		m.updateCheckInfo(config, changeID, details.Failures)
		shouldEnsure = true

	case change.Kind() == recoverCheckKind && new == state.DoneStatus:
		details := mustGetCheckDetails(change)
		if !details.Proceed {
			break
		}
		config := m.state.Cached(recoverConfigKey{change.ID()}).(*plan.Check) // panic if key not present (always should be)
		changeID := performCheckChange(m.state, config)
		m.updateCheckInfo(config, changeID, 0)
		shouldEnsure = true
	}

	if shouldEnsure {
		m.state.EnsureBefore(0) // start new tasks right away
	}
}

func (m *CheckManager) callFailureHandlers(name string) {
	for _, f := range m.failureHandlers {
		f(name)
	}
}

func mustGetCheckDetails(change *state.Change) checkDetails {
	tasks := change.Tasks()
	if len(tasks) != 1 {
		panic(fmt.Sprintf("internal error: %s change %s should have one task", change.Kind(), change.ID()))
	}
	var details checkDetails
	err := tasks[0].Get(checkDetailsAttr, &details)
	if err != nil {
		panic(fmt.Sprintf("internal error: cannot get %s change %s check details: %v", change.Kind(), change.ID(), err))
	}
	return details
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

// newChecker creates a new checker of the configured type. Assumes
// mergeServiceContext has already been called.
func newChecker(config *plan.Check) checker {
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
		return &execChecker{
			name:        config.Name,
			command:     config.Exec.Command,
			environment: config.Exec.Environment,
			userID:      config.Exec.UserID,
			user:        config.Exec.User,
			groupID:     config.Exec.GroupID,
			group:       config.Exec.Group,
			workingDir:  config.Exec.WorkingDir,
		}

	default:
		// This has already been checked when parsing the config.
		panic("internal error: invalid check config")
	}
}

// mergeServiceContext returns the final check configuration with service
// context merged (for exec checks). The original config is copied if needed,
// not modified.
func mergeServiceContext(p *plan.Plan, config *plan.Check) *plan.Check {
	if config.Exec == nil || config.Exec.ServiceContext == "" {
		return config
	}
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
	cpy := config.Copy()
	cpy.Exec.Environment = merged.Environment
	cpy.Exec.UserID = merged.UserID
	cpy.Exec.User = merged.User
	cpy.Exec.Group = merged.Group
	cpy.Exec.GroupID = merged.GroupID
	cpy.Exec.WorkingDir = merged.WorkingDir
	return cpy
}

// Checks returns the list of currently-configured checks and their status,
// ordered by name.
func (m *CheckManager) Checks() ([]*CheckInfo, error) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	infos := make([]*CheckInfo, 0, len(m.checks))
	for _, info := range m.checks {
		info := info // take the address of a new variable each time
		infos = append(infos, &info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

func (m *CheckManager) updateCheckInfo(config *plan.Check, changeID string, failures int) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	status := CheckStatusUp
	if failures >= config.Threshold {
		status = CheckStatusDown
	}
	m.checks[config.Name] = CheckInfo{
		Name:      config.Name,
		Level:     config.Level,
		Status:    status,
		Failures:  failures,
		Threshold: config.Threshold,
		ChangeID:  changeID,
	}
}

func (m *CheckManager) deleteCheckInfo(name string) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	delete(m.checks, name)
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

type checker interface {
	check(ctx context.Context) error
}
