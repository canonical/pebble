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
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/reaper"
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

	err := reaper.Start()
	c.Assert(err, IsNil)
}

func (s *ManagerSuite) TearDownSuite(c *C) {
	err := reaper.Stop()
	c.Assert(err, IsNil)
}

func (s *ManagerSuite) TestChecks(c *C) {
	st := state.New(nil)
	mgr := NewManager(st)
	mgr.PlanChanged(&plan.Plan{
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
	})
	defer stopChecks(c, mgr)

	checks, err := mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk1", Status: "up", Threshold: 3},
		{Name: "chk2", Status: "up", Level: "alive", Threshold: 3},
		{Name: "chk3", Status: "up", Level: "ready", Threshold: 3},
	})

	// Re-configuring should update checks
	mgr.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk4": {
				Name:      "chk4",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk4"},
			},
		},
	})
	checks, err = mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, DeepEquals, []*CheckInfo{
		{Name: "chk4", Status: "up", Threshold: 3},
	})
}

func stopChecks(c *C, mgr *CheckManager) {
	mgr.PlanChanged(&plan.Plan{})
	checks, err := mgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 0)
}

func (s *ManagerSuite) TestTimeout(c *C) {
	st := state.New(nil)
	mgr := NewManager(st)
	mgr.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 25 * time.Millisecond},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: "/bin/sh -c 'echo FOO; sleep 0.05'"},
			},
		},
	})
	defer stopChecks(c, mgr)

	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Status != CheckStatusUp
	})
	c.Check(check.Failures, Equals, 1)
	c.Check(check.Threshold, Equals, 1)

	st.Lock()
	defer st.Unlock()
	changes := st.Changes()
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].Kind(), Equals, "recover-check")
	c.Check(changes[0].Summary(), Equals, `Recover check "chk1"`)
	tasks := changes[0].Tasks()
	c.Assert(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "check-failure")
	c.Check(tasks[0].Summary(), Equals, `Check failure 1 (threshold 1)`)
	c.Assert(tasks[0].Log(), HasLen, 1)
	c.Check(tasks[0].Log()[0], Matches, `2\S+ INFO exec check timed out\nFOO`)
}

func (s *ManagerSuite) TestCheckCanceled(c *C) {
	st := state.New(nil)
	mgr := NewManager(st)
	failureName := ""
	mgr.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c "for i in {1..1000}; do echo x >>%s; sleep 0.005; done"`, tempFile)
	mgr.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: command},
			},
		},
	})

	// Wait for command to start (output file grows in size)
	prevSize := 0
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := ioutil.ReadFile(tempFile)
		if len(b) != prevSize {
			break
		}
		prevSize = len(b)
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
	time.Sleep(50 * time.Millisecond)
	b1, err := ioutil.ReadFile(tempFile)
	c.Assert(err, IsNil)
	time.Sleep(20 * time.Millisecond)
	b2, err := ioutil.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(len(b1), Equals, len(b2))

	// Ensure it didn't trigger failure action
	c.Check(failureName, Equals, "")

	// Ensure it didn't update check failure details (white box testing) or record a change.
	info := check.info()
	c.Check(info.Status, Equals, CheckStatusUp)
	c.Check(info.Failures, Equals, 0)
	c.Check(info.Threshold, Equals, 1)
	st.Lock()
	defer st.Unlock()
	changes := st.Changes()
	c.Assert(changes, HasLen, 0)
}

func (s *ManagerSuite) TestFailures(c *C) {
	st := state.New(nil)
	mgr := NewManager(st)
	failureName := ""
	mgr.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	os.Setenv("FAIL_PEBBLE_TEST", "1")
	mgr.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Period:    plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: `/bin/sh -c '[ -z $FAIL_PEBBLE_TEST ]'`},
			},
		},
	})
	defer stopChecks(c, mgr)

	// Shouldn't have called failure handler after only 1 failures
	check := waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 1
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusUp)
	c.Assert(failureName, Equals, "")
	checkRecoverChange := func(status state.Status, numTasks int) {
		st.Lock()
		defer st.Unlock()
		changes := st.Changes()
		c.Assert(changes, HasLen, 1)
		c.Check(changes[0].Kind(), Equals, "recover-check")
		c.Check(changes[0].Status(), Equals, status)
		tasks := changes[0].Tasks()
		c.Assert(tasks, HasLen, numTasks)
		task := tasks[len(tasks)-1]
		c.Check(task.Kind(), Equals, "check-failure")
		c.Check(task.Status(), Equals, status)
		c.Check(task.Summary(), Equals, fmt.Sprintf(`Check failure %d (threshold 3)`, numTasks))
		c.Assert(task.Log(), HasLen, 1)
		c.Check(task.Log()[0], Matches, `2\S+ INFO exit status 1`)
	}
	checkRecoverChange(state.DoingStatus, 1)

	// Shouldn't have called failure handler after only 2 failures
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 2
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusUp)
	c.Assert(failureName, Equals, "")
	checkRecoverChange(state.DoingStatus, 2)

	// Should have called failure handler and be unhealthy after 3 failures (threshold)
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Failures == 3
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, CheckStatusDown)
	c.Assert(failureName, Equals, "chk1")
	checkRecoverChange(state.DoingStatus, 3)

	// Should reset number of failures if command then succeeds
	failureName = ""
	os.Setenv("FAIL_PEBBLE_TEST", "")
	check = waitCheck(c, mgr, "chk1", func(check *CheckInfo) bool {
		return check.Status == CheckStatusUp
	})
	c.Assert(check.Failures, Equals, 0)
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(failureName, Equals, "")
	checkRecoverChange(state.DoneStatus, 3)
}

func waitCheck(c *C, mgr *CheckManager, name string, f func(check *CheckInfo) bool) *CheckInfo {
	for i := 0; i < 100; i++ {
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
	})
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
	})
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
	})
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
