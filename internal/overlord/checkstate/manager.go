// Package checkstate is the manager and "checkers" for custom health checks.
package checkstate

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

// CheckManager starts and manages the health checks.
type CheckManager struct {
	mutex  sync.Mutex
	checks map[string]*checkData
}

// NewManager creates a new check manager.
func NewManager() *CheckManager {
	return &CheckManager{}
}

// PlanHandler handles updated to the plan (server configuration).
func (m *CheckManager) PlanHandler(p *plan.Plan) {
	err := m.configure(p.Checks)
	if err != nil {
		logger.Noticef("Cannot configure check manager: %v", err)
	}
}

// configure reconfigures the manager with the given check configuration,
// stopping the previous checks and starting the new ones as required.
func (m *CheckManager) configure(checksConfig map[string]*plan.Check) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	logger.Debugf("Configuring check manager (stopping %d, starting %d)",
		len(m.checks), len(checksConfig))

	// First stop existing checks.
	for _, check := range m.checks {
		check.stop()
	}

	// Then configure and start new checks.
	checks := make(map[string]*checkData, len(checksConfig))
	for name, config := range checksConfig {
		check := &checkData{
			config:  config,
			checker: newChecker(config),
			done:    make(chan struct{}),
		}
		checks[name] = check
		go check.loop()
	}
	m.checks = checks
	return nil
}

// Checks returns the list of currently-configured checks and their status,
// ordered by name.
//
// If level is not UnsetLevel, the list of checks is filtered to only include
// checks with the given level. If names is non-empty, the list of checks is
// filtered to only include the named checks.
func (m *CheckManager) Checks(level plan.CheckLevel, names []string) ([]*CheckInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var infos []*CheckInfo
	for _, check := range m.checks {
		if level != plan.UnsetLevel && level != check.config.Level {
			continue
		}
		if len(names) > 0 && !containsString(names, check.config.Name) {
			continue
		}
		infos = append(infos, check.info())
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

func containsString(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// CheckInfo provides status information about a single check.
type CheckInfo struct {
	Name         string
	Healthy      bool
	Failures     int
	LastError    string
	ErrorDetails string
}

// checkData holds state for an active health check.
type checkData struct {
	config  *plan.Check
	checker checker
	done    chan struct{}

	mutex     sync.Mutex
	failures  int
	actionRan bool
	lastErr   error
	cancel    context.CancelFunc
}

type checker interface {
	check(ctx context.Context) error
}

func (c *checkData) loop() {
	logger.Debugf("Check %q starting with period %v", c.config.Name, c.config.Period.Value)
	defer logger.Debugf("Check %q stopped", c.config.Name)

	ticker := time.NewTicker(c.config.Period.Value)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.check()
		case <-c.done:
			return
		}
	}
}

func (c *checkData) check() {
	// Set up context: a timeout or call to c.stop() will cancel the check.
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout.Value)
	defer cancel()
	c.mutex.Lock()
	c.cancel = cancel
	c.mutex.Unlock()

	// Run the check!
	err := c.checker.check(ctx)

	// Lock while we update state, as the manager may access these too.
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cancel = nil
	c.lastErr = err
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Check was stopped, don't trigger failure action.
			logger.Debugf("Check %q canceled in flight", c.config.Name)
			return
		}

		// Track failure, run failure action if "failures" threshold was hit.
		c.failures++
		logger.Noticef("Check %q failure %d (threshold %d): %v",
			c.config.Name, c.failures, c.config.Failures, err)
		if !c.actionRan && c.failures >= c.config.Failures {
			logger.Noticef("Check %q failure threshold %d hit, triggering action",
				c.config.Name, c.config.Failures)
			c.actionRan = true
			// TODO: trigger action
		}
		return
	}
	c.failures = 0
	c.actionRan = false
}

func (c *checkData) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	close(c.done)
}

func (c *checkData) info() *CheckInfo {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	info := &CheckInfo{
		Name:     c.config.Name,
		Healthy:  c.failures == 0,
		Failures: c.failures,
	}
	if c.lastErr != nil {
		info.LastError = c.lastErr.Error()
		switch e := c.lastErr.(type) {
		case *outputError:
			info.ErrorDetails = e.output()
		}
	}
	return info
}
