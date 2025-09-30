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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/metrics"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/planstate"
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
	planMgr  *planstate.PlanManager
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
	layersDir := filepath.Join(c.MkDir(), "layers")
	err = os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)
	s.planMgr, err = planstate.NewManager(layersDir)
	c.Assert(err, IsNil)
	s.overlord.AddManager(s.planMgr)
	s.manager = checkstate.NewManager(s.overlord.State(), s.overlord.TaskRunner(), s.planMgr)
	s.planMgr.AddChangeListener(s.manager.PlanChanged)
	s.overlord.AddManager(s.manager)
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
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Level:     "alive",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
			},
			"chk3": {
				Name:      "chk3",
				Override:  "replace",
				Level:     "ready",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
			},
		},
	})

	// Wait for expected checks to be started.
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "enabled", Status: "up", Level: "alive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Level: "ready", Threshold: 3},
	})

	// Re-configuring should update checks.
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk4": {
				Name:      "chk4",
				Override:  "merge",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk4"},
			},
		},
	})

	// Wait for checks to be updated.
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk4", Startup: "enabled", Status: "up", Threshold: 3},
	})
}

func (s *ManagerSuite) TestTimeout(c *C) {
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 25 * time.Millisecond},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: "/bin/sh -c 'echo FOO; sleep 0.05'"},
			},
		},
	})

	check := waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return true
	})
	performChangeID := check.ChangeID

	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Status == checkstate.CheckStatusDown && check.ChangeID != performChangeID
	})
	c.Assert(check.Failures, Equals, 1)
	c.Assert(check.Threshold, Equals, 1)

	// Ensure that the original perform-check task logged an error.
	st := s.overlord.State()
	st.Lock()
	change := st.Change(performChangeID)
	status := change.Status()
	st.Unlock()
	c.Assert(status, Equals, state.ErrorStatus)
	c.Assert(lastTaskLog(st, performChangeID), Matches, ".* ERROR check timed out after 25ms")
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
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 1,
				Exec:      &plan.ExecCheck{Command: command},
			},
		},
	})

	// Wait for command to start (output file is not zero in size)
	for i := 0; ; i++ {
		if i >= 1000 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := os.ReadFile(tempFile)
		if len(b) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Cancel the check in-flight
	s.manager.PlanChanged(plan.NewPlan())
	waitChecks(c, s.manager, nil)

	// Ensure command was terminated (output file didn't grow in size)
	b1, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	time.Sleep(20 * time.Millisecond)
	b2, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(len(b2), Equals, len(b1))

	// Ensure it didn't trigger failure action
	c.Check(failureName, Equals, "")
}

func (s *ManagerSuite) TestFailures(c *C) {
	const threshold = 10

	var notifies atomic.Int32
	s.manager.NotifyCheckFailed(func(name string) {
		notifies.Add(1)
	})
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond},
				Threshold: threshold,
				Exec: &plan.ExecCheck{
					Command: fmt.Sprintf(`/bin/sh -c 'echo details >/dev/stderr; [ ! -f %s ]'`, testPath),
				},
			},
		},
	})

	// Shouldn't have called failure handler after only a couple of failures
	check := waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures >= 1 && check.Failures < threshold
	})
	originalChangeID := check.ChangeID
	c.Assert(check.Threshold, Equals, threshold)
	c.Assert(check.Status, Equals, checkstate.CheckStatusUp)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, ".* ERROR exit status 1; details")
	c.Assert(notifies.Load(), Equals, int32(0))

	// Should have called failure handler and be unhealthy after 10 failures (threshold)
	c.Assert(changeData(c, s.overlord.State(), check.ChangeID), DeepEquals, map[string]string{"check-name": "chk1"})
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures >= threshold && check.ChangeID != originalChangeID
	})
	c.Assert(check.Threshold, Equals, threshold)
	c.Assert(check.Status, Equals, checkstate.CheckStatusDown)
	c.Assert(notifies.Load(), Equals, int32(1))
	recoverChangeID := check.ChangeID

	// Should log failures in recover-check mode
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures > threshold
	})
	c.Assert(check.Threshold, Equals, threshold)
	c.Assert(check.Status, Equals, checkstate.CheckStatusDown)
	c.Assert(notifies.Load(), Equals, int32(1))
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, ".* ERROR exit status 1; details")
	c.Assert(check.ChangeID, Equals, recoverChangeID)

	// Should reset number of failures if command then succeeds
	err = os.Remove(testPath)
	c.Assert(err, IsNil)
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Status == checkstate.CheckStatusUp && check.ChangeID != recoverChangeID
	})
	c.Assert(check.Failures, Equals, 0)
	c.Assert(check.Threshold, Equals, threshold)
	c.Assert(notifies.Load(), Equals, int32(1))
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Equals, "")
	c.Assert(changeData(c, s.overlord.State(), check.ChangeID), DeepEquals, map[string]string{"check-name": "chk1"})
}

func (s *ManagerSuite) TestFailuresBelowThreshold(c *C) {
	const threshold = 10

	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond},
				Threshold: threshold,
				Exec: &plan.ExecCheck{
					Command: fmt.Sprintf(`/bin/sh -c '[ ! -f %s ]'`, testPath),
				},
			},
		},
	})

	// Wait for a failure (below the threshold)
	check := waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures >= 1 && check.Failures < threshold
	})
	c.Assert(check.Status, Equals, checkstate.CheckStatusUp)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, ".* ERROR exit status 1")

	// Should reset number of failures if command then succeeds
	err = os.Remove(testPath)
	c.Assert(err, IsNil)
	check = waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures == 0
	})
	c.Assert(check.Status, Equals, checkstate.CheckStatusUp)
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Matches, `.* INFO succeeded after \d+ failure.*`)
}

func (s *ManagerSuite) TestPlanChangedSmarts(c *C) {
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
			},
			"chk3": {
				Name:      "chk3",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
			},
		},
	})

	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 3)
	var changeIDs []string
	for _, check := range checks {
		changeIDs = append(changeIDs, check.ChangeID)
	}

	// Modify plan: chk1 unchanged, chk2 modified, chk3 deleted.
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 6,
				Exec:      &plan.ExecCheck{Command: "echo chk2 modified"},
			},
		},
	})

	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "enabled", Status: "up", Threshold: 6},
	})
	checks, err = s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 2)
	var newChangeIDs []string
	for _, check := range checks {
		newChangeIDs = append(newChangeIDs, check.ChangeID)
	}
	c.Assert(changeIDs[0], Equals, newChangeIDs[0])
	c.Assert(changeIDs[1], Not(Equals), newChangeIDs[1])
	c.Assert(newChangeIDs[0], Not(Equals), newChangeIDs[1])
}

func (s *ManagerSuite) TestPlanChangedServiceContext(c *C) {
	origPlan := &plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {
				Name:       "svc1",
				Override:   "replace",
				Command:    "dummy1",
				WorkingDir: "/tmp",
			},
			"svc2": {
				Name:       "svc2",
				Override:   "replace",
				Command:    "dummy2",
				WorkingDir: "/tmp",
			},
		},
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec: &plan.ExecCheck{
					Command:        "echo chk1",
					ServiceContext: "svc1",
				},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec: &plan.ExecCheck{
					Command:        "echo chk2",
					ServiceContext: "svc2",
				},
			},
		},
	}
	s.manager.PlanChanged(origPlan)
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 2)
	var changeIDs []string
	for _, check := range checks {
		changeIDs = append(changeIDs, check.ChangeID)
	}

	// Modify plan: chk1 service context unchanged, chk2 service context changed.
	s.manager.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": origPlan.Services["svc1"],
			"svc2": {
				Name:       "svc2",
				Override:   "merge",
				Command:    "dummy2",
				WorkingDir: c.MkDir(),
			},
		},
		Checks: map[string]*plan.Check{
			"chk1": origPlan.Checks["chk1"],
			"chk2": origPlan.Checks["chk2"],
		},
	})

	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err = s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks, HasLen, 2)
	var newChangeIDs []string
	for _, check := range checks {
		newChangeIDs = append(newChangeIDs, check.ChangeID)
	}
	c.Assert(changeIDs[0], Equals, newChangeIDs[0])
	c.Assert(changeIDs[1], Not(Equals), newChangeIDs[1])
	c.Assert(newChangeIDs[0], Not(Equals), newChangeIDs[1])
}

func (s *ManagerSuite) TestSuccessNoLog(c *C) {
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c 'echo -n x >>%s'`, tempFile)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 10 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: command},
			},
		},
	})

	// Wait for check to run at least twice
	for i := 0; ; i++ {
		if i >= 1000 {
			c.Fatalf("failed waiting for check to run")
		}
		b, _ := os.ReadFile(tempFile)
		if len(b) >= 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Ensure it didn't log anything to the task on success.
	check := waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return true
	})
	c.Assert(lastTaskLog(s.overlord.State(), check.ChangeID), Equals, "")
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

	var checks []*checkstate.CheckInfo
	for start := time.Now(); time.Since(start) < timeout; {
		var err error
		checks, err = mgr.Checks()
		c.Assert(err, IsNil)
		for _, check := range checks {
			if check.Name == name && f(check) {
				return check
			}
		}
		time.Sleep(time.Millisecond)
	}

	for i, check := range checks {
		c.Logf("check %d: %#v", i, *check)
	}
	c.Fatalf("timed out waiting for check %q", name)
	return nil
}

func waitChecks(c *C, mgr *checkstate.CheckManager, expected []*checkstate.CheckInfo) {
	var checks []*checkstate.CheckInfo
	for start := time.Now(); time.Since(start) < 10*time.Second; {
		var err error
		checks, err = mgr.Checks()
		c.Assert(err, IsNil)
		for _, check := range checks {
			check.ChangeID = "" // clear change ID to avoid comparing it
		}
		if len(checks) == 0 && len(expected) == 0 || reflect.DeepEqual(checks, expected) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	for i, check := range checks {
		c.Logf("check %d: %#v", i, *check)
	}
	c.Fatal("timed out waiting for checks to settle")
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

func changeData(c *C, st *state.State, changeID string) map[string]string {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	var data map[string]string
	err := chg.Get("notice-data", &data)
	c.Assert(err, IsNil)
	return data
}

func (s *ManagerSuite) TestStartChecks(c *C) {
	origLayer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
				Startup:   plan.CheckStartupDisabled,
			},
			"chk3": {
				Name:      "chk3",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
				Startup:   plan.CheckStartupEnabled,
			},
		},
	}
	err := s.planMgr.AppendLayer(origLayer, false)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(s.planMgr.Plan())
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	var originalChangeIDs []string
	for _, check := range checks {
		originalChangeIDs = append(originalChangeIDs, check.ChangeID)
	}

	changed, err := s.manager.StartChecks([]string{"chk1", "chk2"})
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "up", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	c.Assert(err, IsNil)
	c.Assert(changed, DeepEquals, []string{"chk2"})
	checks, err = s.manager.Checks()
	c.Assert(err, IsNil)
	// chk1 and chk3 should still have the same change ID, chk2 should have a new one.
	c.Assert(checks[0].ChangeID, Equals, originalChangeIDs[0])
	c.Assert(checks[1].ChangeID, Not(Equals), originalChangeIDs[1])
	c.Assert(checks[2].ChangeID, Equals, originalChangeIDs[2])
	// chk2's new Change should be a running perform-check.
	st := s.overlord.State()
	st.Lock()
	change := st.Change(checks[1].ChangeID)
	status := change.Status()
	st.Unlock()
	c.Assert(status, Matches, "Do.*")
	c.Assert(change.Kind(), Equals, "perform-check")
}

func (s *ManagerSuite) TestStartChecksNotFound(c *C) {
	origLayer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
		},
	}
	err := s.planMgr.AppendLayer(origLayer, false)
	c.Assert(err, IsNil)
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})

	changed, err := s.manager.StartChecks([]string{"chk1", "chk2"})
	var notFoundErr *checkstate.ChecksNotFound
	c.Assert(errors.As(err, &notFoundErr), Equals, true)
	c.Assert(notFoundErr.Names, DeepEquals, []string{"chk2"})
	c.Assert(changed, IsNil)
}

func (s *ManagerSuite) TestStopChecks(c *C) {
	origLayer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
				Startup:   plan.CheckStartupDisabled,
			},
			"chk3": {
				Name:      "chk3",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
				Startup:   plan.CheckStartupEnabled,
			},
		},
	}
	err := s.planMgr.AppendLayer(origLayer, false)
	c.Assert(err, IsNil)

	// Run an Ensure pass to kick the check tasks into Doing status.
	st := s.overlord.State()
	st.EnsureBefore(0)

	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})

	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	var chk1ChangeID, chk3ChangeID string
	for _, check := range checks {
		switch check.Name {
		case "chk1":
			chk1ChangeID = check.ChangeID
		case "chk3":
			chk3ChangeID = check.ChangeID
		}
	}
	c.Assert(chk1ChangeID, Not(Equals), "")
	c.Assert(chk3ChangeID, Not(Equals), "")

	start := time.Now()
	for {
		if time.Since(start) > 3*time.Second {
			c.Fatal("timed out waiting for check changes to go to Doing")
		}
		st.Lock()
		chk1Status := st.Change(chk1ChangeID).Status()
		chk3Status := st.Change(chk3ChangeID).Status()
		st.Unlock()
		if chk1Status == state.DoingStatus && chk3Status == state.DoingStatus {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	changed, err := s.manager.StopChecks([]string{"chk1", "chk2"})
	c.Assert(err, IsNil)
	c.Assert(changed, DeepEquals, []string{"chk1"})

	// Run an Ensure pass to actually stop the checks.
	st.EnsureBefore(0)

	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "inactive", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})

	// chk3 should still have the same change ID, chk1 and chk2 should not have one.
	checks, err = s.manager.Checks()
	c.Assert(err, IsNil)
	c.Assert(checks[0].ChangeID, Equals, "")
	c.Assert(checks[1].ChangeID, Equals, "")
	c.Assert(checks[2].ChangeID, Equals, chk3ChangeID)

	// chk1's old change should go to Done.
	start = time.Now()
	for {
		if time.Since(start) > 3*time.Second {
			c.Fatal("timed out waiting for check changes to go to Done")
		}
		st.Lock()
		chk1Status := st.Change(chk1ChangeID).Status()
		st.Unlock()
		if chk1Status == state.DoneStatus {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *ManagerSuite) TestStopChecksNotFound(c *C) {
	origLayer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
		},
	}
	err := s.planMgr.AppendLayer(origLayer, false)
	c.Assert(err, IsNil)
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})

	changed, err := s.manager.StopChecks([]string{"chk1", "chk2"})
	var notFoundErr *checkstate.ChecksNotFound
	c.Assert(errors.As(err, &notFoundErr), Equals, true)
	c.Assert(notFoundErr.Names, DeepEquals, []string{"chk2"})
	c.Assert(changed, IsNil)
}

func (s *ManagerSuite) TestReplan(c *C) {
	origLayer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk1"},
			},
			"chk2": {
				Name:      "chk2",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk2"},
				Startup:   plan.CheckStartupDisabled,
			},
			"chk3": {
				Name:      "chk3",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: "echo chk3"},
				Startup:   plan.CheckStartupEnabled,
			},
		},
	}
	err := s.planMgr.AppendLayer(origLayer, false)
	c.Assert(err, IsNil)
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	s.manager.StopChecks([]string{"chk1"})
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "inactive", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	var originalChangeIDs []string
	for _, check := range checks {
		originalChangeIDs = append(originalChangeIDs, check.ChangeID)
	}

	s.overlord.State().Lock()
	s.manager.Replan()
	s.overlord.State().Unlock()
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
		{Name: "chk2", Startup: "disabled", Status: "inactive", Threshold: 3},
		{Name: "chk3", Startup: "enabled", Status: "up", Threshold: 3},
	})
	c.Assert(err, IsNil)
	checks, err = s.manager.Checks()
	c.Assert(err, IsNil)
	// chk3 should still have the same change ID, chk1 should have a new one,
	// and chk2 should not have one.
	c.Assert(checks[0].ChangeID, Not(Equals), originalChangeIDs[0])
	c.Assert(checks[1].ChangeID, Equals, "")
	c.Assert(checks[2].ChangeID, Equals, originalChangeIDs[2])
	// chk1's new Change should be a running perform-check.
	st := s.overlord.State()
	st.Lock()
	change := st.Change(checks[0].ChangeID)
	c.Assert(change, NotNil)
	status := change.Status()
	st.Unlock()
	c.Assert(status, Matches, "Do.*")
	c.Assert(change.Kind(), Equals, "perform-check")
}

func (s *ManagerSuite) TestMetricsCheckSuccess(c *C) {
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c 'echo -n x >>%s'`, tempFile)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 10 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: command},
				Startup:   plan.CheckStartupEnabled,
			},
		},
	})

	// Wait for check to run at least twice
	for i := 0; ; i++ {
		if i >= 1000 {
			c.Fatalf("failed waiting for check to run")
		}
		b, _ := os.ReadFile(tempFile)
		if len(b) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	buf := new(bytes.Buffer)
	writer := metrics.NewOpenTelemetryWriter(buf)
	s.manager.WriteMetrics(writer)
	expectedRegex := `
# HELP pebble_check_up Whether the health check is up \(1\) or not \(0\)
# TYPE pebble_check_up gauge
pebble_check_up{check="chk1"} 1

# HELP pebble_check_success_count Number of times the check has succeeded
# TYPE pebble_check_success_count counter
pebble_check_success_count{check="chk1"} \d+

# HELP pebble_check_failure_count Number of times the check has failed
# TYPE pebble_check_failure_count counter
pebble_check_failure_count{check="chk1"} 0

`[1:]
	c.Assert(buf.String(), Matches, expectedRegex)
}

func (s *ManagerSuite) TestMetricsCheckFailure(c *C) {
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 20 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond},
				Threshold: 3,
				Exec: &plan.ExecCheck{
					Command: fmt.Sprintf(`/bin/sh -c 'echo details >/dev/stderr; [ ! -f %s ]'`, testPath),
				},
			},
		},
	})

	// After 2 failures, check is still up, pebble_check_success_count counter is 0,
	// pebble_check_failure_count is 2.
	waitCheck(c, s.manager, "chk1", func(check *checkstate.CheckInfo) bool {
		return check.Failures == 2
	})

	buf := new(bytes.Buffer)
	writer := metrics.NewOpenTelemetryWriter(buf)
	s.manager.WriteMetrics(writer)
	expectedRegex := `
# HELP pebble_check_up Whether the health check is up \(1\) or not \(0\)
# TYPE pebble_check_up gauge
pebble_check_up{check="chk1"} 1

# HELP pebble_check_success_count Number of times the check has succeeded
# TYPE pebble_check_success_count counter
pebble_check_success_count{check="chk1"} 0

# HELP pebble_check_failure_count Number of times the check has failed
# TYPE pebble_check_failure_count counter
pebble_check_failure_count{check="chk1"} \d+

`[1:]
	c.Assert(buf.String(), Matches, expectedRegex)
}

func (s *ManagerSuite) TestMetricsInactiveCheck(c *C) {
	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	command := fmt.Sprintf(`/bin/sh -c 'echo -n x >>%s'`, tempFile)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: 10 * time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec:      &plan.ExecCheck{Command: command},
				Startup:   plan.CheckStartupDisabled,
			},
		},
	})

	buf := new(bytes.Buffer)
	writer := metrics.NewOpenTelemetryWriter(buf)
	s.manager.WriteMetrics(writer)
	// Inactive check's pebble_check_up metric is not reported.
	expected := `
# HELP pebble_check_success_count Number of times the check has succeeded
# TYPE pebble_check_success_count counter
pebble_check_success_count{check="chk1"} 0

# HELP pebble_check_failure_count Number of times the check has failed
# TYPE pebble_check_failure_count counter
pebble_check_failure_count{check="chk1"} 0

`[1:]
	c.Assert(buf.String(), Equals, expected)
}

func (s *ManagerSuite) TestRefreshCheck(c *C) {
	chk1 := &plan.Check{
		Name:      "chk1",
		Override:  "replace",
		Period:    plan.OptionalDuration{Value: time.Second},
		Threshold: 3,
		Exec:      &plan.ExecCheck{Command: "echo chk1"},
	}
	layer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": chk1,
		},
	}
	err := s.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(s.planMgr.Plan())
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	originalChangeID := checks[0].ChangeID

	checkInfo, err := s.manager.RefreshCheck(context.Background(), chk1)
	c.Assert(err, IsNil)

	c.Assert(*checkInfo, DeepEquals, checkstate.CheckInfo{
		Name:      "chk1",
		Level:     "",
		Startup:   "enabled",
		Status:    "up",
		Successes: 1,
		Failures:  0,
		Threshold: 3,
		ChangeID:  originalChangeID,
	})
}

func (s *ManagerSuite) TestRefreshCheckFailure(c *C) {
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	chk1 := &plan.Check{
		Name:      "chk1",
		Override:  "replace",
		Period:    plan.OptionalDuration{Value: time.Second},
		Timeout:   plan.OptionalDuration{Value: time.Second},
		Threshold: 3,
		Exec:      &plan.ExecCheck{Command: fmt.Sprintf(`/bin/sh -c 'echo details >/dev/stderr; [ ! -f %s ]'`, testPath)},
	}
	layer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": chk1,
		},
	}
	err = s.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(s.planMgr.Plan())
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})
	checks, err := s.manager.Checks()
	c.Assert(err, IsNil)
	originalChangeID := checks[0].ChangeID
	checkInfo, err := s.manager.RefreshCheck(context.Background(), chk1)
	c.Assert(err, ErrorMatches, "exit status 1; details")
	c.Assert(*checkInfo, DeepEquals, checkstate.CheckInfo{
		Name:      "chk1",
		Level:     "",
		Startup:   "enabled",
		Status:    "up",
		Failures:  1,
		Threshold: 3,
		ChangeID:  originalChangeID,
	})
}

func (s *ManagerSuite) TestRefreshStoppedCheck(c *C) {
	chk1 := &plan.Check{
		Name:      "chk1",
		Override:  "replace",
		Period:    plan.OptionalDuration{Value: time.Second},
		Timeout:   plan.OptionalDuration{Value: time.Second},
		Threshold: 3,
		Exec:      &plan.ExecCheck{Command: "echo chk1"},
	}
	layer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": chk1,
		},
	}
	err := s.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(s.planMgr.Plan())
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})
	_, err = s.manager.Checks()
	c.Assert(err, IsNil)

	changed, err := s.manager.StopChecks([]string{"chk1"})
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "inactive", Threshold: 3},
	})
	c.Assert(err, IsNil)
	c.Assert(changed, DeepEquals, []string{"chk1"})
	_, err = s.manager.Checks()
	c.Assert(err, IsNil)

	checkInfo, err := s.manager.RefreshCheck(context.Background(), chk1)
	c.Assert(err, IsNil)
	c.Assert(*checkInfo, DeepEquals, checkstate.CheckInfo{
		Name:      "chk1",
		Level:     "",
		Startup:   "enabled",
		Status:    "inactive",
		Failures:  0,
		Threshold: 3,
	})
}

func (s *ManagerSuite) TestRefreshStoppedCheckFailure(c *C) {
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	chk1 := &plan.Check{
		Name:      "chk1",
		Override:  "replace",
		Period:    plan.OptionalDuration{Value: time.Second},
		Timeout:   plan.OptionalDuration{Value: time.Second},
		Threshold: 3,
		Exec:      &plan.ExecCheck{Command: fmt.Sprintf(`/bin/sh -c 'echo details >/dev/stderr; [ ! -f %s ]'`, testPath)},
	}
	layer := &plan.Layer{
		Checks: map[string]*plan.Check{
			"chk1": chk1,
		},
	}
	err = s.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(s.planMgr.Plan())
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "up", Threshold: 3},
	})
	_, err = s.manager.Checks()
	c.Assert(err, IsNil)

	changed, err := s.manager.StopChecks([]string{"chk1"})
	waitChecks(c, s.manager, []*checkstate.CheckInfo{
		{Name: "chk1", Startup: "enabled", Status: "inactive", Threshold: 3},
	})
	c.Assert(err, IsNil)
	c.Assert(changed, DeepEquals, []string{"chk1"})
	_, err = s.manager.Checks()
	c.Assert(err, IsNil)

	checkInfo, err := s.manager.RefreshCheck(context.Background(), chk1)
	c.Assert(err, ErrorMatches, "exit status 1; details")
	c.Assert(*checkInfo, DeepEquals, checkstate.CheckInfo{
		Name:      "chk1",
		Level:     "",
		Startup:   "enabled",
		Status:    "inactive",
		Failures:  0,
		Threshold: 3,
	})
}

func (s *ManagerSuite) TestChecksSuccesses(c *C) {
	testPath := c.MkDir() + "/test"
	err := os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	s.manager.PlanChanged(&plan.Plan{
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  "replace",
				Period:    plan.OptionalDuration{Value: time.Millisecond},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: 3,
				Exec: &plan.ExecCheck{
					Command: fmt.Sprintf(`/bin/sh -c '[ -f %s ]'`, testPath),
				},
			},
		},
	})

	// Wait for "successes" to go up (to a number we'll be sure to be under when we reset).
	numSuccesses := 250
	check := waitCheck(c, s.manager, "chk1", func(chk *checkstate.CheckInfo) bool {
		return chk.Successes >= numSuccesses
	})

	// Remove the file to make the check start failing.
	err = os.Remove(testPath)
	c.Assert(err, IsNil)
	failedCheck := waitCheck(c, s.manager, "chk1", func(chk *checkstate.CheckInfo) bool {
		return chk.Status == checkstate.CheckStatusDown
	})
	c.Assert(failedCheck.Failures >= 3, Equals, true)
	c.Assert(failedCheck.Successes >= check.Successes, Equals, true)
	failingCheck := waitCheck(c, s.manager, "chk1", func(chk *checkstate.CheckInfo) bool {
		return chk.Failures > 3
	})
	c.Assert(failingCheck.Successes, Equals, failedCheck.Successes)

	// Reinstate the file to make the check succeed and go into "perform" mode again.
	err = os.WriteFile(testPath, nil, 0o644)
	c.Assert(err, IsNil)
	waitCheck(c, s.manager, "chk1", func(chk *checkstate.CheckInfo) bool {
		c.Assert(chk.Successes, Not(Equals), 0)
		return chk.Successes >= 2 && chk.Successes < numSuccesses
	})
}
