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

package checkstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
)

func Test(t *testing.T) {
	TestingT(t)
}

type ManagerSuite struct {
	overlord *overlord.Overlord
	manager  *checkstate.CheckManager
}

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

	s.overlord = overlord.Fake()
	s.manager = checkstate.NewManager(s.overlord.State(), s.overlord.TaskRunner())
	s.overlord.AddManager(s.overlord.TaskRunner())
	err = s.overlord.StartUp()
	c.Assert(err, IsNil)
	s.overlord.Loop()
}

func (s *ManagerSuite) TearDownTest(c *C) {
	s.overlord.Stop()

	err := reaper.Stop()
	c.Assert(err, IsNil)
}

func (s *ManagerSuite) TestChecks(c *C) {
	s.manager.PlanChanged(&plan.Plan{
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

	// Wait for expected checks to be started.
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Status: "up", Threshold: 3},
		{Name: "chk2", Status: "up", Level: "alive", Threshold: 3},
		{Name: "chk3", Status: "up", Level: "ready", Threshold: 3},
	})

	// Re-configuring should update checks.
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk4": {
				Name:      "chk4",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk4"},
			},
		},
	})

	// Wait for checks to be updated.
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk4", Status: "up", Threshold: 3},
	})
}

func (s *ManagerSuite) TestTimeout(c *C) {
	s.manager.PlanChanged(&plan.Plan{
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

	check := waitCheck(c, s.manager, "chk1", nil)
	originalChangeID := check.ChangeID

	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Status == checkstate.CheckStatusDown
	})
	c.Assert(check.Failures, Equals, 1)
	c.Assert(check.Threshold, Equals, 1)
	c.Assert(check.ChangeID, Not(Equals), originalChangeID)

	// Ensure that the original perform-check task logs an error.
	// We need to wait for perform-check change to be ready (Error status)
	// so we can read the log without a race.
	st := s.overlord.State()
	st.Lock()
	change := st.Change(originalChangeID)
	st.Unlock()
	select {
	case <-change.Ready():
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for change %s", originalChangeID)
	}
	st.Lock()
	status := change.Status()
	st.Unlock()
	c.Assert(status, Equals, state.ErrorStatus)
	c.Assert(lastTaskLog(st, originalChangeID), Matches, ".* ERROR exec check timed out")
}

func (s *ManagerSuite) TestCheckCanceled(c *C) {
	failureName := ""
	s.manager.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c "for i in {1..1000}; do echo x >>%s; sleep 0.005; done"`, tempFile)
	s.manager.PlanChanged(&plan.Plan{
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

	// Cancel the check in-flight
	s.manager.PlanChanged(&plan.Plan{})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 0)

	// Ensure command was terminated (output file didn't grow in size)
	b1, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	time.Sleep(20 * time.Millisecond)
	b2, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(len(b1), Equals, len(b2))

	// Ensure it didn't trigger failure action
	c.Check(failureName, Equals, "")
}

func (s *ManagerSuite) TestFailures(c *C) {
	failureName := ""
	s.manager.NotifyCheckFailed(func(name string) {
		failureName = name
	})
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(&plan.Plan{
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
	})

	check := waitCheck(c, s.manager, "chk1", nil)
	originalChangeID := check.ChangeID

	// Shouldn't have called failure handler after only 1 failure
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures == 1
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, checkstate.CheckStatusUp)
	c.Assert(failureName, Equals, "")

	// Ensure that the original perform-check task logs an error.
	// We need to wait for perform-check change to be ready (Error status)
	// so we can read the log without a race.
	st := s.overlord.State()
	st.Lock()
	change := st.Change(originalChangeID)
	st.Unlock()
	select {
	case <-change.Ready():
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for change %s", originalChangeID)
	}
	c.Assert(lastTaskLog(s.overlord.State(), originalChangeID), Matches, ".* exit status 1")

	recoverChangeID := check.ChangeID
	c.Assert(recoverChangeID, Not(Equals), originalChangeID)

	// Shouldn't have called failure handler after only 2 failures
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures == 2
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, checkstate.CheckStatusUp)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, ".* exit status 1")
	c.Assert(failureName, Equals, "")

	// Should have called failure handler and be unhealthy after 3 failures (threshold)
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures == 3
	})
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(check.Status, Equals, checkstate.CheckStatusDown)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, ".* exit status 1")
	c.Assert(failureName, Equals, "chk1")

	// Should reset number of failures if command then succeeds
	failureName = ""
	err = os.Remove(testPath)
	c.Assert(err, IsNil)
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Status == checkstate.CheckStatusUp
	})
	c.Assert(check.Failures, Equals, 0)
	c.Assert(check.Threshold, Equals, 3)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Equals, "")
	c.Assert(failureName, Equals, "")

	newPerformChangeID := check.ChangeID
	c.Assert(newPerformChangeID, Not(Equals), recoverChangeID)
}

// waitCheck is a time based approach to wait for a checker run to complete.
// The timeout value does not impact the general time it takes for tests to
// complete, but determines a worst case waiting period before giving up.
// The timeout value must take into account single core or very busy machines
// so it makes sense to pick a conservative number here as failing a test
// due to a busy test resource is more extensive than waiting a few more
// seconds.
func waitCheck(c *C, mgr *checkstate.CheckManager, name string, f func(check *checkstate.CheckInfo) bool) *checkstate.CheckInfo {
	// Worst case waiting time for checker run(s) to complete. This
	// period should be much longer than the longest
	// check timeout value.
	timeout := 10 * time.Second

	for start := time.Now(); time.Since(start) < timeout; {
		checks, err := mgr.Checks()
		c.Assert(err, IsNil)
		for _, check := range checks {
			if check.Name == name && (f == nil || f(check)) {
				return check
			}
		}
		time.Sleep(time.Millisecond)
	}

	c.Fatalf("timed out waiting for check %q", name)
	return nil
}

func waitChecks(c *C, mgr *checkstate.CheckManager, expected []*checkstate.CheckInfo) []*checkstate.CheckInfo {
	for start := time.Now(); time.Since(start) < 10*time.Second; {
		checks, err := mgr.Checks()
		c.Assert(err, IsNil)
		for _, check := range checks {
			check.ChangeID = "" // clear change ID to avoid comparing it
		}
		if reflect.DeepEqual(checks, expected) {
			return checks
		}
		time.Sleep(time.Millisecond)
	}
	c.Fatalf("timed out waiting for checks to settle to %#v", expected)
	return nil
}

func lastTaskLog(st *state.State, changeID string) string {
	st.Lock()
	defer st.Unlock()

	change := st.Change(changeID)
	tasks := change.Tasks()
	if len(tasks) < 1 {
		return ""
	}
	logs := tasks[0].Log()
	if len(logs) < 1 {
		return ""
	}
	return logs[len(logs)-1]
}
