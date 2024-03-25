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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
)

func Test(t *testing.T) {
	TestingT(t)
}

type ManagerSuite struct{}

var _ = Suite(&ManagerSuite{})

var setLoggerOnce sync.Once

func (s *ManagerSuite) SetUpSuite(c *C) {
	// This can happen in parallel with tests if -test.count=N with N>1 is specified.
	setLoggerOnce.Do(func() {
		logger.SetLogger(logger.New(os.Stderr, "[test] "))
	})
}

func (s *ManagerSuite) SetUpTest(c *C) {
	err := reaper.Start()
	c.Assert(err, IsNil)
}

func (s *ManagerSuite) TearDownTest(c *C) {
	err := reaper.Stop()
	c.Assert(err, IsNil)
}

func (s *ManagerSuite) TestChecks(c *C) {
	mgr := NewManager()
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Level:     "alive",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
			},
			"chk3": {
				Name:      "chk3",
				Level:     "ready",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
			},
		},
	}))
	defer stopChecks(c, mgr)

	checks, err := mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk1", Status: "up", Threshold: 3},
		{Name: "chk2", Status: "up", Level: "alive", Threshold: 3},
		{Name: "chk3", Status: "up", Level: "ready", Threshold: 3},
	})

	// Re-configuring should update checks
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Checks: map[string]*plan.Check{
			"chk4": {
				Name:      "chk4",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk4"},
			},
		},
	}))
	checks, err = mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk4", Status: "up", Threshold: 3},
	})
}

func stopChecks(c *C, mgr *CheckManager) {
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{}))
	checks, err := mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 0)
}

func (s *ManagerSuite) TestTimeout(c *C) {
	mgr := NewManager()
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 25 * time.Millisecond},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: "/bin/sh -c 'echo FOO; sleep 0.05'"},
			},
		},
	}))
	defer stopChecks(c, mgr)

	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Status != CheckStatusUp
	})
	c.Assert(check.Failures, Equals, 1)
	c.Assert(check.Threshold, Equals, 1)
	c.Assert(check.LastError, Equals, "exec check timed out")
}

func (s *ManagerSuite) TestCheckCanceled(c *C) {
	mgr := NewManager()
	failureName := ""
	mgr.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c "for i in {1..1000}; do echo x >>%s; sleep 0.005; done"`, tempFile)
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: command},
			},
		},
	}))

	// Wait for command to start (output file is not zero in size)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := os.ReadFile(tempFile)
		if len(b) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// For the little bit of white box testing below (we can't use mgr.Checks
	// later, because the checks will have stopped by that point).
	mgr.mutex.Lock()
	check := mgr.checks["chk1"]
	mgr.mutex.Unlock()

	// Cancel the check in-flight
	stopChecks(c, mgr)

	// Ensure command was terminated (output file didn't grow in size)
	b1, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	time.Sleep(20 * time.Millisecond)
	b2, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(len(b1), Equals, len(b2))

	// Ensure it didn't trigger failure action
	c.Check(failureName, Equals, "")

	// Ensure it didn't update check failure details (white box testing)
	info := check.info()
	c.Check(info.Status, Equals, CheckStatusUp)
	c.Check(info.Failures, Equals, 0)
	c.Check(info.Threshold, Equals, 1)
	c.Check(info.LastError, Equals, "")
	c.Check(info.ErrorDetails, Equals, "")
}

func (s *ManagerSuite) TestFailures(c *C) {
	mgr := NewManager()
	failureName := ""
	mgr.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	mgr.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond},
				Threshold: 3,
				Exec: &plan.ExecCheck{
					Command: fmt.Sprintf(`/bin/sh -c '[ ! -f %s ]'`, testPath),
				},
			},
		},
	}))
	defer stopChecks(c, mgr)

	// Shouldn't have called failure handler after only 1 failures
	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 1
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusUp)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "")

	// Shouldn't have called failure handler after only 2 failures
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 2
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusUp)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "")

	// Should have called failure handler and be unhealthy after 3 failures (threshold)
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 3
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusDown)
	c.Assert(check.LastError, Matches, "exit status 1")
	c.Assert(failureName, Equals, "chk1")

	// Should reset number of failures if command then succeeds
	failureName = ""
	err = os.Remove(testPath)
	c.Assert(err, IsNil)
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Status == CheckStatusUp
	})
	c.Assert(check.Failures, Equals, 0)
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.LastError, Equals, "")
	c.Assert(failureName, Equals, "")
}

// waitCheck is a time based approach to wait for a checker run to complete.
// The timeout value does not impact the general time it takes for tests to
// complete, but determines a worst case waiting period before giving up.
// The timeout value must take into account single core or very busy machines
// so it makes sense to pick a conservative number here as failing a test
// due to a busy test resource is more extensive than waiting a few more
// seconds.
func waitCheck(c *C, mgr *CheckManager, name string, f func(check *CheckInfo) bool) *CheckInfo {
	// Worst case waiting time for checker run(s) to complete. This
	// period should be much longer (10x is good) than the longest
	// check timeout value.
	timeout := time.Second * 10

	for start := time.Now(); time.Since(start) < timeout; {
		checks, err := mgr.Checks()
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

func (s *CheckersSuite) TestNewChecker(c *C) {
	chk := newChecker(&plan.Check{
		Name: "http",
		HTTP: &plan.HTTPCheck{
			URL:     "https://example.com/foo",
			Headers: map[string]string{"k": "v"},
		},
	}, nil)
	http, ok := chk.(*httpChecker)
	c.Assert(ok, Equals, true)
	c.Check(http.name, Equals, "http")
	c.Check(http.url, Equals, "https://example.com/foo")
	c.Check(http.headers, DeepEquals, map[string]string{"k": "v"})

	chk = newChecker(&plan.Check{
		Name: "tcp",
		TCP: &plan.TCPCheck{
			Port: 80,
			Host: "localhost",
		},
	}, nil)
	tcp, ok := chk.(*tcpChecker)
	c.Assert(ok, Equals, true)
	c.Check(tcp.name, Equals, "tcp")
	c.Check(tcp.port, Equals, 80)
	c.Check(tcp.host, Equals, "localhost")

	userID, groupID := 100, 200
	chk = newChecker(&plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:     "sleep 1",
			Environment: map[string]string{"k": "v"},
			UserID:      &userID,
			User:        "user",
			GroupID:     &groupID,
			Group:       "group",
			WorkingDir:  "/working/dir",
		},
	}, nil)
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Assert(exec.name, Equals, "exec")
	c.Assert(exec.command, Equals, "sleep 1")
	c.Assert(exec.environment, DeepEquals, map[string]string{"k": "v"})
	c.Assert(exec.userID, Equals, &userID)
	c.Assert(exec.user, Equals, "user")
	c.Assert(exec.groupID, Equals, &groupID)
	c.Assert(exec.workingDir, Equals, "/working/dir")
}

func (s *CheckersSuite) TestExecContextNoOverride(c *C) {
	svcUserID, svcGroupID := 10, 20
	chk := newChecker(&plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:        "sleep 1",
			ServiceContext: "svc1",
		},
	}, plan.NewCombinedPlan(&plan.Layer{Services: map[string]*plan.Service{
		"svc1": {
			Name:        "svc1",
			Environment: map[string]string{"k": "x", "a": "1"},
			UserID:      &svcUserID,
			User:        "svcuser",
			GroupID:     &svcGroupID,
			Group:       "svcgroup",
			WorkingDir:  "/working/svc",
		},
	}}))
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Check(exec.name, Equals, "exec")
	c.Check(exec.command, Equals, "sleep 1")
	c.Check(exec.environment, DeepEquals, map[string]string{"k": "x", "a": "1"})
	c.Check(exec.userID, DeepEquals, &svcUserID)
	c.Check(exec.user, Equals, "svcuser")
	c.Check(exec.groupID, DeepEquals, &svcGroupID)
	c.Check(exec.workingDir, Equals, "/working/svc")
}

func (s *CheckersSuite) TestExecContextOverride(c *C) {
	userID, groupID := 100, 200
	svcUserID, svcGroupID := 10, 20
	chk := newChecker(&plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:        "sleep 1",
			ServiceContext: "svc1",
			Environment:    map[string]string{"k": "v"},
			UserID:         &userID,
			User:           "user",
			GroupID:        &groupID,
			Group:          "group",
			WorkingDir:     "/working/dir",
		},
	}, plan.NewCombinedPlan(&plan.Layer{Services: map[string]*plan.Service{
		"svc1": {
			Name:        "svc1",
			Environment: map[string]string{"k": "x", "a": "1"},
			UserID:      &svcUserID,
			User:        "svcuser",
			GroupID:     &svcGroupID,
			Group:       "svcgroup",
			WorkingDir:  "/working/svc",
		},
	}}))
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Check(exec.name, Equals, "exec")
	c.Check(exec.command, Equals, "sleep 1")
	c.Check(exec.environment, DeepEquals, map[string]string{"k": "v", "a": "1"})
	c.Check(exec.userID, DeepEquals, &userID)
	c.Check(exec.user, Equals, "user")
	c.Check(exec.groupID, DeepEquals, &groupID)
	c.Check(exec.workingDir, Equals, "/working/dir")
}
