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
	"strings"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/metrics"
	"github.com/canonical/pebble/internals/overlord/planstate"
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
	state   *state.State
	planMgr *planstate.PlanManager

	failureHandlers []FailureFunc

	checksLock sync.Mutex
	checks     map[string]*checkData
}

// FailureFunc is the type of function called when a failure action is triggered.
type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager(s *state.State, runner *state.TaskRunner, planMgr *planstate.PlanManager) *CheckManager {
	manager := &CheckManager{
		state:   s,
		checks:  make(map[string]*checkData),
		planMgr: planMgr,
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
			var configKey any
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
				// We exclude any checks that have changed from startup:enabled
				// to startup:disabled, because these should now be inactive and
				// only started when explicitly requested.
				// If the plan doesn't have an explicit startup value, it
				// defaults to enabled, for backwards compatibility.
				if newConfig.Startup == plan.CheckStartupDisabled &&
					(oldConfig.Startup == plan.CheckStartupEnabled || oldConfig.Startup == plan.CheckStartupUnknown) {
					continue
				}
				// Check is in old and new plans and has been modified.
				newOrModified[details.Name] = true
			}
			change.Abort()
			m.deleteCheckData(details.Name)
			shouldEnsure = true
		}
	}

	// Also find checks that are new (in new plan but not in old one) and have
	// `startup` set to `enabled` (or not explicitly set).
	for _, config := range newPlan.Checks {
		if !existingChecks[config.Name] {
			if config.Startup == plan.CheckStartupEnabled || config.Startup == plan.CheckStartupUnknown {
				newOrModified[config.Name] = true
			} else {
				// Check is new and should be inactive - no need to start it,
				// but we need to add it to the list of existing checks.
				m.updateCheckData(config, "", 0)
			}
		}
	}

	// Start new or modified checks.
	for _, config := range newPlan.Checks {
		if newOrModified[config.Name] {
			merged := mergeServiceContext(newPlan, config)
			changeID := performCheckChange(m.state, merged)
			m.updateCheckData(config, changeID, 0)
			shouldEnsure = true
		}
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
		m.updateCheckData(config, changeID, details.Failures)
		shouldEnsure = true

	case change.Kind() == recoverCheckKind && new == state.DoneStatus:
		details := mustGetCheckDetails(change)
		if !details.Proceed {
			break
		}
		config := m.state.Cached(recoverConfigKey{change.ID()}).(*plan.Check) // panic if key not present (always should be)
		changeID := performCheckChange(m.state, config)
		m.updateCheckData(config, changeID, 0)
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
	for _, checkData := range m.checks {
		info := CheckInfo{
			Name:      checkData.name,
			Level:     checkData.level,
			Startup:   checkData.startup,
			Status:    checkData.status,
			Failures:  checkData.failures,
			Threshold: checkData.threshold,
			ChangeID:  checkData.changeID,
		}
		infos = append(infos, &info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

func (m *CheckManager) ensureCheck(name string) *checkData {
	check, ok := m.checks[name]
	if !ok {
		check = &checkData{
			name:    name,
			refresh: make(chan refreshInfo),
		}
		m.checks[name] = check
	}
	return check
}

func (m *CheckManager) updateCheckData(config *plan.Check, changeID string, failures int) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	status := CheckStatusUp
	if changeID == "" {
		status = CheckStatusInactive
	} else if failures >= config.Threshold {
		status = CheckStatusDown
	}
	startup := config.Startup
	if startup == plan.CheckStartupUnknown {
		startup = plan.CheckStartupEnabled
	}

	check := m.ensureCheck(config.Name)
	check.level = config.Level
	check.startup = startup
	check.status = status
	check.failures = failures
	check.threshold = config.Threshold
	check.changeID = changeID
}

func (m *CheckManager) incSuccessCount(config *plan.Check) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	check := m.ensureCheck(config.Name)
	check.successCount += 1
}

func (m *CheckManager) incFailureCount(config *plan.Check) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	check := m.ensureCheck(config.Name)
	check.failureCount += 1
}

func (m *CheckManager) deleteCheckData(name string) {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	delete(m.checks, name)
}

// CheckInfo provides status information about a single check.
type CheckInfo struct {
	Name      string
	Level     plan.CheckLevel
	Startup   plan.CheckStartup
	Status    CheckStatus
	Failures  int
	Threshold int
	ChangeID  string
}

type refreshInfo struct {
	ctx    context.Context
	result chan error
}

// checkData holds the metrics and other data for a single check.
type checkData struct {
	name         string
	level        plan.CheckLevel
	startup      plan.CheckStartup
	status       CheckStatus
	failures     int
	threshold    int
	changeID     string
	successCount int64
	failureCount int64
	refresh      chan refreshInfo
}

type CheckStatus string

const (
	CheckStatusUp       CheckStatus = "up"
	CheckStatusDown     CheckStatus = "down"
	CheckStatusInactive CheckStatus = "inactive"
)

type checker interface {
	check(ctx context.Context) error
}

func (c *checkData) writeMetric(writer metrics.Writer) error {
	// Don't list any inactive checks because they don't have an up or down status.
	if c.status != CheckStatusInactive {
		checkUp := 0
		if c.status == CheckStatusUp {
			checkUp = 1
		}
		err := writer.Write(metrics.Metric{
			Name:       "pebble_check_up",
			Type:       metrics.TypeGaugeInt,
			ValueInt64: int64(checkUp),
			Comment:    "Whether the health check is up (1) or not (0)",
			Labels:     []metrics.Label{metrics.NewLabel("check", c.name)},
		})
		if err != nil {
			return err
		}
	}

	err := writer.Write(metrics.Metric{
		Name:       "pebble_check_success_count",
		Type:       metrics.TypeCounterInt,
		ValueInt64: c.successCount,
		Comment:    "Number of times the check has succeeded",
		Labels:     []metrics.Label{metrics.NewLabel("check", c.name)},
	})
	if err != nil {
		return err
	}

	err = writer.Write(metrics.Metric{
		Name:       "pebble_check_failure_count",
		Type:       metrics.TypeCounterInt,
		ValueInt64: c.failureCount,
		Comment:    "Number of times the check has failed",
		Labels:     []metrics.Label{metrics.NewLabel("check", c.name)},
	})
	if err != nil {
		return err
	}

	return nil
}

// WriteMetrics collects and writes metrics for all checks to the provided writer.
func (m *CheckManager) WriteMetrics(writer metrics.Writer) error {
	m.checksLock.Lock()
	defer m.checksLock.Unlock()

	names := make([]string, 0, len(m.checks))
	for name := range m.checks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := m.checks[name]
		err := info.writeMetric(writer)
		if err != nil {
			return err
		}
	}

	return nil
}

// ChecksNotFound is the error returned by StartChecks or StopChecks when a check
// with the specified name is not found in the plan.
type ChecksNotFound struct {
	Names []string
}

func (e *ChecksNotFound) Error() string {
	sort.Strings(e.Names)
	return fmt.Sprintf("cannot find checks in plan: %s", strings.Join(e.Names, ", "))
}

// StartChecks starts the checks with the specified names, if not already
// running, and returns the checks that did need to be started.
func (m *CheckManager) StartChecks(checks []string) (started []string, err error) {
	currentPlan := m.planMgr.Plan()

	// If any check specified is not in the plan, return an error.
	var missing []string
	for _, name := range checks {
		if _, ok := currentPlan.Checks[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, &ChecksNotFound{Names: missing}
	}

	m.state.Lock()
	defer m.state.Unlock()

	for _, name := range checks {
		check := currentPlan.Checks[name] // We know this is ok because we checked it above.
		m.checksLock.Lock()
		checkData, ok := m.checks[name]
		m.checksLock.Unlock()
		if !ok {
			// This will be rare: either a PlanChanged is running and this is
			// between the checkData being removed and then being added back, or the
			// check is new to the plan and PlanChanged hasn't finished yet.
			logger.Noticef("check %s is in the plan but not known to the manager", name)
			continue
		}
		// If the check is already running, skip it.
		if checkData.changeID != "" {
			continue
		}
		changeID := performCheckChange(m.state, check)
		m.updateCheckData(check, changeID, 0)
		started = append(started, check.Name)
	}

	return started, nil
}

// StopChecks stops the checks with the specified names, if currently running,
// and returns the checks that did need to be stopped.
func (m *CheckManager) StopChecks(checks []string) (stopped []string, err error) {
	currentPlan := m.planMgr.Plan()

	// If any check specified is not in the plan, return an error.
	var missing []string
	for _, name := range checks {
		if _, ok := currentPlan.Checks[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, &ChecksNotFound{Names: missing}
	}

	m.state.Lock()
	defer m.state.Unlock()

	for _, name := range checks {
		check := currentPlan.Checks[name] // We know this is ok because we checked it above.
		m.checksLock.Lock()
		checkData, ok := m.checks[name]
		m.checksLock.Unlock()
		if !ok {
			// This will be rare: either a PlanChanged is running and this is
			// between the info being removed and then being added back, or the
			// check is new to the plan and PlanChanged hasn't finished yet.
			logger.Noticef("check %s is in the plan but not known to the manager", name)
			continue
		}
		// If the check is not running, skip it.
		if checkData.changeID == "" {
			continue
		}
		change := m.state.Change(checkData.changeID)
		if change != nil {
			change.Abort()
			change.SetStatus(state.AbortStatus)
			stopped = append(stopped, check.Name)
		}
		// We pass in the current number of failures so that it remains the
		// same, so that people can inspect what the state of the check was when
		// it was stopped. The status of the check will be "inactive", but the
		// failure count combined with the threshold will give the full picture.
		m.updateCheckData(check, "", checkData.failures)
	}

	return stopped, nil
}

// Replan handles starting "startup: enabled" checks when a replan occurs.
// Checks that are "startup: disabled" but are already running do not get
// stopped in a replan.
// The state lock must be held when calling this method.
func (m *CheckManager) Replan() {
	currentPlan := m.planMgr.Plan()

	for _, check := range currentPlan.Checks {
		m.checksLock.Lock()
		checkData, ok := m.checks[check.Name]
		m.checksLock.Unlock()
		if !ok {
			// This will be rare: either a PlanChanged is running and this is
			// between the checkData being removed and then being added back, or the
			// check is new to the plan and PlanChanged hasn't finished yet.
			logger.Noticef("check %s is in the plan but not known to the manager", check.Name)
			continue
		}
		if check.Startup == plan.CheckStartupDisabled {
			continue
		}
		// If the check is already running, skip it.
		if checkData.changeID != "" {
			continue
		}
		changeID := performCheckChange(m.state, check)
		m.updateCheckData(check, changeID, 0)
	}
}

// RefreshCheck runs a check immediately.
func (m *CheckManager) RefreshCheck(ctx context.Context, check *plan.Check) (*CheckInfo, error) {
	m.checksLock.Lock()
	data := m.ensureCheck(check.Name)
	refresh := data.refresh
	changeID := data.changeID
	m.checksLock.Unlock()

	getCheckInfo := func() *CheckInfo {
		m.checksLock.Lock()
		defer m.checksLock.Unlock()
		checkData := m.ensureCheck(check.Name)
		info := CheckInfo{
			Name:      checkData.name,
			Level:     checkData.level,
			Startup:   checkData.startup,
			Status:    checkData.status,
			Failures:  checkData.failures,
			Threshold: checkData.threshold,
			ChangeID:  checkData.changeID,
		}
		return &info
	}

	// If the check is stopped, run the check directly without using changes and tasks.
	if changeID == "" {
		chk := newChecker(check)
		err := runCheck(ctx, chk, check.Timeout.Value)
		if err != nil {
			return getCheckInfo(), fmt.Errorf("%s", errorDetails(err))
		}
		return getCheckInfo(), nil
	}
	if refresh == nil {
		panic(fmt.Sprintf("internal error: refresh channel not initialized for check %q", data.name))
	}

	result := make(chan error)
	select {
	case refresh <- refreshInfo{ctx, result}:
	case <-ctx.Done():
		return getCheckInfo(), ctx.Err()
	}
	select {
	case err := <-result:
		if err != nil {
			return getCheckInfo(), fmt.Errorf("%s", errorDetails(err))
		}
		return getCheckInfo(), nil
	case <-ctx.Done():
		return getCheckInfo(), ctx.Err()
	}
}
