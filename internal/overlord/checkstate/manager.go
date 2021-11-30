// Package checkstate is the manager and "checkers" for custom health checks.
package checkstate

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/strutil"
)

// CheckManager starts and manages the health checks.
type CheckManager struct {
	mutex           sync.Mutex
	checks          map[string]*checkData
	failureHandlers []FailureFunc
}

type FailureFunc func(name string)

// NewManager creates a new check manager.
func NewManager() *CheckManager {
	return &CheckManager{}
}

// AddFailureHandler adds f to the list of "failure handlers", functions that
// are called whenever a check hits its failure threshold.
func (m *CheckManager) AddFailureHandler(f FailureFunc) {
	m.failureHandlers = append(m.failureHandlers, f)
}

// Configure handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *CheckManager) Configure(p *plan.Plan) {
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

// Checks returns the list of currently-configured checks and their status,
// ordered by name.
//
// If level is not UnsetLevel, the list of checks is filtered to the checks
// with the given level. Because "ready" implies "alive", if level is
// AliveLevel, checks with level "ready" are included too.
//
// If names is non-empty, the list of checks is filtered to the named checks.
func (m *CheckManager) Checks(level plan.CheckLevel, names []string) ([]*CheckInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var infos []*CheckInfo
	for _, check := range m.checks {
		levelMatch := level == plan.UnsetLevel ||
			level == check.config.Level ||
			level == plan.AliveLevel && check.config.Level == plan.ReadyLevel
		namesMatch := len(names) == 0 || strutil.ListContains(names, check.config.Name)
		if levelMatch && namesMatch {
			infos = append(infos, check.info())
		}
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
	Healthy      bool
	Failures     int
	LastError    string
	ErrorDetails string
}

// checkData holds state for an active health check.
type checkData struct {
	config  *plan.Check
	checker checker
	ctx     context.Context
	cancel  context.CancelFunc

	mutex     sync.Mutex
	failures  int
	action    FailureFunc
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
			c.check()
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

func (c *checkData) check() {
	// Run the check with a timeout.
	ctx, cancel := context.WithTimeout(c.ctx, c.config.Timeout.Value)
	defer cancel()
	err := c.checker.check(ctx)

	// Lock while we update state, as the manager may access these too.
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err != nil {
		if ctx.Err() == context.Canceled {
			// Check was stopped, don't trigger failure action.
			logger.Debugf("Check %q canceled in flight", c.config.Name)
			return
		}

		// Track failure, run failure action if "failures" threshold was hit.
		c.lastErr = err
		c.failures++
		logger.Noticef("Check %q failure %d (threshold %d): %v",
			c.config.Name, c.failures, c.config.Failures, err)
		if !c.actionRan && c.failures >= c.config.Failures {
			logger.Noticef("Check %q failure threshold %d hit, triggering action",
				c.config.Name, c.config.Failures)
			c.action(c.config.Name)
			c.actionRan = true
		}
		return
	}
	c.lastErr = nil
	c.failures = 0
	c.actionRan = false
}

func (c *checkData) info() *CheckInfo {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	info := &CheckInfo{
		Name:     c.config.Name,
		Level:    c.config.Level,
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
