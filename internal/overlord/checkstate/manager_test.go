// Test the check manager

package checkstate

import (
	"os"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

type ManagerSuite struct{}

var _ = Suite(&ManagerSuite{})

var setLoggerOnce sync.Once

func (s *ManagerSuite) SetUpSuite(c *C) {
	// This can happen in parallel with tests if -test.count=N with N>1 is specified.
	setLoggerOnce.Do(func() {
		logger.SetLogger(logger.New(os.Stderr, "[test] "))
	})
}

func (s *ManagerSuite) TestChecks(c *C) {
	mgr := NewManager()
	mgr.Configure(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:   "chk1",
				Period: plan.OptionalDuration{Value: time.Second},
				Exec:   &plan.ExecCheckConfig{Command: "echo chk1"},
			},
			"chk2": {
				Name:   "chk2",
				Level:  "alive",
				Period: plan.OptionalDuration{Value: time.Second},
				Exec:   &plan.ExecCheckConfig{Command: "echo chk2"},
			},
			"chk3": {
				Name:   "chk3",
				Period: plan.OptionalDuration{Value: time.Second},
				Exec:   &plan.ExecCheckConfig{Command: "echo chk3"},
			},
		},
	})
	defer stopChecks(c, mgr)

	// Returns all checks with no filters
	checks, err := mgr.Checks("", nil)
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk1", Healthy: true},
		{Name: "chk2", Healthy: true},
		{Name: "chk3", Healthy: true},
	})

	// Level filter works
	checks, err = mgr.Checks(plan.AliveLevel, nil)
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk2", Healthy: true},
	})

	// Check names filter works
	checks, err = mgr.Checks("", []string{"chk3", "chk2"})
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk2", Healthy: true},
		{Name: "chk3", Healthy: true},
	})

	// If both filters specified, should be an AND
	checks, err = mgr.Checks(plan.AliveLevel, []string{"chk3", "chk2"})
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk2", Healthy: true},
	})

	// Re-configuring should update checks
	mgr.Configure(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk4": {
				Name:   "chk4",
				Period: plan.OptionalDuration{Value: time.Second},
				Exec:   &plan.ExecCheckConfig{Command: "echo chk4"},
			},
		},
	})
	checks, err = mgr.Checks("", nil)
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk4", Healthy: true},
	})
}

func stopChecks(c *C, mgr *CheckManager) {
	mgr.Configure(&plan.Plan{})
	checks, err := mgr.Checks("", nil)
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 0)
}

func (s *ManagerSuite) TestTimeout(c *C) {
	mgr := NewManager()
	mgr.Configure(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:     "chk1",
				Period:   plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:  plan.OptionalDuration{Value: 10 * time.Millisecond},
				Failures: 1,
				Exec:     &plan.ExecCheckConfig{Command: "/bin/sh -c 'echo FOO; sleep 0.02'"},
			},
		},
	})
	defer stopChecks(c, mgr)

	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return !check.Healthy
	})
	c.Assert(check.Failures, Equals, 1)
	c.Assert(check.LastError, Equals, "exec check timed out")
	c.Assert(check.ErrorDetails, Equals, "FOO\n")
}

func (s *ManagerSuite) TestFailures(c *C) {
	mgr := NewManager()
	failureName := ""
	mgr.AddFailureHandler(func(name string) {
		failureName = name
	})
	os.Setenv("FAIL_PEBBLE_TEST", "1")
	mgr.Configure(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:     "chk1",
				Period:   plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:  plan.OptionalDuration{Value: 10 * time.Millisecond},
				Failures: 3,
				Exec:     &plan.ExecCheckConfig{Command: `/bin/sh -c '[ -z $FAIL_PEBBLE_TEST ]'`},
			},
		},
	})
	defer stopChecks(c, mgr)

	// Shouldn't have called failure handler after only 1 failures
	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 1
	})
	c.Assert(check.Healthy, Equals, false)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "")

	// Shouldn't have called failure handler after only 2 failures
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 2
	})
	c.Assert(check.Healthy, Equals, false)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "")

	// Should have called failure handler after 3 failures (threshold)
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 3
	})
	c.Assert(check.Healthy, Equals, false)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "chk1")

	// Should reset number of failures if command then succeeds
	failureName = ""
	os.Setenv("FAIL_PEBBLE_TEST", "")
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Healthy
	})
	c.Assert(check.Failures, Equals, 0)
	c.Assert(check.LastError, Equals, "")
	c.Assert(failureName, Equals, "")
}

func waitCheck(c *C, mgr *CheckManager, name string, f func(check *CheckInfo) bool) *CheckInfo {
	for i := 0; i < 100; i++ {
		checks, err := mgr.Checks("", nil)
		c.Assert(err, IsNil)
		for _, check := range checks {
			if check.Name == name && f(check) {
				return check
			}
		}
		time.Sleep(time.Millisecond)
	}
	c.Fatalf("timed out waiting for check %q", name)
	return nil
}
