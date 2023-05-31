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
	"sort"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
)

// CheckManager starts and manages the health checks.
type CheckManager struct {
	mutex           sync.Mutex
	checks          map[string]*checkData
	failureHandlers []FailureFunc
}

// FailureFunc is the type of function called when a failure action is triggered.
type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager() *CheckManager {
	return &CheckManager{}
}

// NotifyCheckFailed adds f to the list of functions that are called whenever
// a check hits its failure threshold.
func (m *CheckManager) NotifyCheckFailed(f FailureFunc) {
	m.failureHandlers = append(m.failureHandlers, f)
}

// PlanChanged handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *CheckManager) PlanChanged(p *plan.Plan) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	logger.Debugf("Configuring check manager (stopping %d, starting %d)",
		len(m.checks), len(p.Checks))

	// First stop existing checks.
	for _, check := range m.checks {
		check.cancel()
	}

	// Then configure and start new checks.
	checks := make(map[string]*checkData, len(p.Checks))
	for name, config := range p.Checks {
		ctx, cancel := context.WithCancel(context.Background())
		check := &checkData{
			config:  config,
			checker: newChecker(config),
			ctx:     ctx,
			cancel:  cancel,
			action:  m.callFailureHandlers,
		}
		checks[name] = check
		go check.loop()
	}
	m.checks = checks
}

func (m *CheckManager) callFailureHandlers(name string) {
	for _, f := range m.failureHandlers {
		f(name)
	}
}

// newChecker creates a new checker of the configured type.
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

// Checks returns the list of currently-configured checks and their status,
// ordered by name.
func (m *CheckManager) Checks() ([]*CheckInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	infos := make([]*CheckInfo, 0, len(m.checks))
	for _, check := range m.checks {
		infos = append(infos, check.info())
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

// CheckInfo provides status information about a single check.
type CheckInfo struct {
	Name         string
	Level        plan.CheckLevel
	Status       CheckStatus
	Failures     int
	Threshold    int
	LastError    string
	ErrorDetails string
}

type CheckStatus string

const (
	CheckStatusUp   CheckStatus = "up"
	CheckStatusDown CheckStatus = "down"
)

// checkData holds state for an active health check.
type checkData struct {
	config  *plan.Check
	checker checker
	ctx     context.Context
	cancel  context.CancelFunc
	action  FailureFunc

	mutex     sync.Mutex
	failures  int
	actionRan bool
	lastErr   error
}

type checker interface {
	check(ctx context.Context) error
}

func (c *checkData) loop() {
	logger.Debugf("Check %q starting with period %v", c.config.Name, c.config.Period.Value)

	ticker := time.NewTicker(c.config.Period.Value)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.runCheck()
			if c.ctx.Err() != nil {
				// Don't re-run check in edge case where period is short and
				// in-flight check was cancelled.
				return
			}
		case <-c.ctx.Done():
			logger.Debugf("Check %q stopped: %v", c.config.Name, c.ctx.Err())
			return
		}
	}
}

func (c *checkData) runCheck() {
	// Run the check with a timeout.
	ctx, cancel := context.WithTimeout(c.ctx, c.config.Timeout.Value)
	defer cancel()
	err := c.checker.check(ctx)

	// Lock while we update state, as the manager may access these too.
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err == nil {
		// Successful check
		c.lastErr = nil
		c.failures = 0
		c.actionRan = false
		return
	}

	if ctx.Err() == context.Canceled {
		// Check was stopped, don't trigger failure action.
		logger.Debugf("Check %q canceled in flight", c.config.Name)
		return
	}

	// Track failure, run failure action if "failures" threshold was hit.
	c.lastErr = err
	c.failures++
	logger.Noticef("Check %q failure %d (threshold %d): %v",
		c.config.Name, c.failures, c.config.Threshold, err)
	if !c.actionRan && c.failures >= c.config.Threshold {
		logger.Noticef("Check %q failure threshold %d hit, triggering action",
			c.config.Name, c.config.Threshold)
		c.action(c.config.Name)
		c.actionRan = true
	}
}

// info returns user-facing check information for use in Checks (and tests).
func (c *checkData) info() *CheckInfo {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	info := &CheckInfo{
		Name:      c.config.Name,
		Level:     c.config.Level,
		Status:    CheckStatusUp,
		Failures:  c.failures,
		Threshold: c.config.Threshold,
	}
	if c.failures >= c.config.Threshold {
		info.Status = CheckStatusDown
	}
	if c.lastErr != nil {
		info.LastError = c.lastErr.Error()
		if d, ok := c.lastErr.(interface{ Details() string }); ok {
			info.ErrorDetails = d.Details()
		}
	}
	return info
}
