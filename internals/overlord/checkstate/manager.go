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

	planLock sync.Mutex
	plan     *plan.Plan

	healthLock sync.Mutex
	health     map[string]HealthInfo
}

// FailureFunc is the type of function called when a failure action is triggered.
type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager(s *state.State, runner *state.TaskRunner) *CheckManager {
	manager := &CheckManager{
		state:  s,
		health: make(map[string]HealthInfo),
	}

	// Health check changes can be long-running; ensure they don't get pruned.
	s.RegisterPendingChangeByAttr(noPruneAttr, func(change *state.Change) bool {
		return true
	})

	runner.AddHandler(performCheckKind, manager.doPerformCheck, nil)
	runner.AddHandler(recoverCheckKind, manager.doRecoverCheck, nil)

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

	// Update local reference to plan.
	m.planLock.Lock() // always acquire locks in same order (state lock, then plan lock)
	oldPlan := m.plan
	m.plan = newPlan
	m.planLock.Unlock()

	if oldPlan == nil {
		oldPlan = &plan.Plan{}
	}
	shouldEnsure := false
	newOrModified := make(map[string]bool, len(newPlan.Checks))

	// Abort all currently-running checks.
	for _, change := range m.state.Changes() {
		switch change.Kind() {
		case performCheckKind, recoverCheckKind:
			if change.IsReady() {
				// Skip check changes that have finished already.
				continue
			}
			details := mustGetCheckDetails(change)
			oldConfig, inOld := oldPlan.Checks[details.Name]
			newConfig, inNew := newPlan.Checks[details.Name]
			if inOld && inNew {
				if reflect.DeepEqual(oldConfig, newConfig) {
					// Don't restart check if its configuration hasn't changed.
					continue
				}
				// Check is in old and new plans and has been modified.
				newOrModified[details.Name] = true
			}
			change.Abort()
			m.deleteHealthInfo(details.Name)
			shouldEnsure = true
		}
	}

	// Also find checks that are new (in new plan but not in old one).
	for _, config := range newPlan.Checks {
		if oldPlan.Checks[config.Name] == nil {
			newOrModified[config.Name] = true
		}
	}

	// Start new or modified checks.
	for _, config := range newPlan.Checks {
		if newOrModified[config.Name] {
			performCheckChange(m.state, config)
			m.updateHealthInfo(config, 0)
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

func (m *CheckManager) changeStatusChanged(chg *state.Change, old, new state.Status) {
	// Always acquire locks in same order (state lock, then plan lock).
	// The state engine has already acquired the state lock at this point.
	m.planLock.Lock()
	plan := m.plan
	m.planLock.Unlock()

	shouldEnsure := false
	switch {
	case chg.Kind() == performCheckKind && new == state.ErrorStatus:
		details := mustGetCheckDetails(chg)
		config, inPlan := plan.Checks[details.Name]
		if details.Proceed && inPlan {
			recoverCheckChange(m.state, config, details.Failures)
			shouldEnsure = true
		}

	case chg.Kind() == recoverCheckKind && new == state.DoneStatus:
		details := mustGetCheckDetails(chg)
		config, inPlan := plan.Checks[details.Name]
		if details.Proceed && inPlan {
			performCheckChange(m.state, config)
			shouldEnsure = true
		}
	}

	if shouldEnsure {
		m.state.EnsureBefore(0) // start new tasks right away
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
	m.state.Lock()
	defer m.state.Unlock()

	// Populate name, number of failures, and change ID from state.
	var infos []*CheckInfo
	for _, change := range m.state.Changes() {
		switch change.Kind() {
		case performCheckKind, recoverCheckKind:
			if change.IsReady() {
				// Skip check changes that have finished already.
				continue
			}
			details := mustGetCheckDetails(change)
			infos = append(infos, &CheckInfo{
				Name:     details.Name,
				Failures: details.Failures,
				ChangeID: change.ID(),
			})
		}
	}

	// Populate other details from plan.
	m.planLock.Lock() // always acquire locks in same order (state lock, then plan lock)
	plan := m.plan
	m.planLock.Unlock()
	for _, info := range infos {
		config, ok := plan.Checks[info.Name]
		if !ok {
			continue
		}
		info.Status = CheckStatusUp
		info.Threshold = config.Threshold
		info.Level = config.Level
		if info.Failures >= info.Threshold {
			info.Status = CheckStatusDown
		}
	}

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

type checker interface {
	check(ctx context.Context) error
}
