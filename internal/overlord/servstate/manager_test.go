// Copyright (c) 2014-2020 Canonical Ltd
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

package servstate_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/checkstate"
	"github.com/canonical/pebble/internal/overlord/restart"
	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/testutil"
)

const (
	shortOkayWait = 50 * time.Millisecond
	shortKillWait = 100 * time.Millisecond
	shortFailWait = 200 * time.Millisecond
)

func Test(t *testing.T) { TestingT(t) }

type S struct {
	testutil.BaseTest

	dir          string
	log          string
	logBuffer    bytes.Buffer
	logBufferMut sync.Mutex

	st *state.State

	manager    *servstate.ServiceManager
	runner     *state.TaskRunner
	stopDaemon chan struct{}
}

var _ = Suite(&S{})

var planLayer1 = `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo test1 | tee -a %s; sleep 300"
        startup: enabled
        requires:
            - test2
        before:
            - test2

    test2:
        override: replace
        command: /bin/sh -c "echo test2 | tee -a %s; sleep 300"
`

var planLayer2 = `
services:
    test3:
        override: replace
        command: some-bad-command

    test4:
        override: replace
        command: echo -e 'too-fast\nsecond line'

    test5:
        override: replace
        command: /bin/sh -c "sleep 300"
        user: nobody
        group: nogroup
`

var planLayer3 = `
services:
    test2:
        override: merge
        command: /bin/sh -c "echo test2b | tee -a %s; sleep 300"
`

var setLoggerOnce sync.Once

func (s *S) SetUpSuite(c *C) {
	// This can happen in parallel with tests if -test.count=N with N>1 is specified.
	setLoggerOnce.Do(func() {
		logger.SetLogger(logger.New(os.Stderr, "[test] "))
	})
}

func (s *S) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.st = state.New(nil)

	s.dir = c.MkDir()
	os.Mkdir(filepath.Join(s.dir, "layers"), 0755)

	s.log = filepath.Join(s.dir, "log.txt")
	data := fmt.Sprintf(planLayer1, s.log, s.log)
	err := ioutil.WriteFile(filepath.Join(s.dir, "layers", "001-base.yaml"), []byte(data), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.dir, "layers", "002-two.yaml"), []byte(planLayer2), 0644)
	c.Assert(err, IsNil)

	s.logBufferMut.Lock()
	s.logBuffer.Reset()
	s.logBufferMut.Unlock()
	logOutput := writerFunc(func(p []byte) (int, error) {
		s.logBufferMut.Lock()
		defer s.logBufferMut.Unlock()
		return s.logBuffer.Write(p)
	})

	s.runner = state.NewTaskRunner(s.st)
	s.stopDaemon = make(chan struct{})
	manager, err := servstate.NewManager(s.st, s.runner, s.dir, logOutput, testRestarter{s.stopDaemon})
	c.Assert(err, IsNil)
	s.manager = manager

	restore := servstate.FakeOkayWait(shortOkayWait)
	s.AddCleanup(restore)
	restore = servstate.FakeKillWait(shortKillWait, shortFailWait)
	s.AddCleanup(restore)
}

func (s *S) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

type testRestarter struct {
	ch chan struct{}
}

func (r testRestarter) HandleRestart(t restart.RestartType) {
	close(r.ch)
}

func (s *S) assertLog(c *C, expected string) {
	s.logBufferMut.Lock()
	defer s.logBufferMut.Unlock()
	data, err := ioutil.ReadFile(s.log)
	if os.IsNotExist(err) {
		c.Fatal("Services have not run")
	}
	c.Assert(err, IsNil)
	c.Assert(string(data), Matches, "(?s)"+expected)
	c.Assert(s.logBuffer.String(), Matches, "(?s)"+expected)
}

func (s *S) logBufferString() string {
	s.logBufferMut.Lock()
	defer s.logBufferMut.Unlock()
	str := s.logBuffer.String()
	s.logBuffer.Reset()
	return str
}

func (s *S) TestDefaultServiceNames(c *C) {
	services, err := s.manager.DefaultServiceNames()
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []string{"test1", "test2"})
}

func (s *S) ensure(c *C, n int) {
	for i := 0; i < n; i++ {
		s.runner.Ensure()
		s.runner.Wait()
	}
}

func (s *S) startServices(c *C, services []string, nEnsure int) *state.Change {
	s.st.Lock()
	ts, err := servstate.Start(s.st, services)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Num to ensure may be more than one due to the cross-task dependencies.
	s.ensure(c, nEnsure)

	return chg
}

func (s *S) stopServices(c *C, services []string, nEnsure int) *state.Change {
	s.st.Lock()
	ts, err := servstate.Stop(s.st, services)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Stop test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Num to ensure may be more than one due to the cross-task dependencies.
	s.ensure(c, nEnsure)

	return chg
}

func (s *S) startTestServices(c *C) {
	chg := s.startServices(c, []string{"test1", "test2"}, 2)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	s.assertLog(c, ".*test1\n.*test2\n")

	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 2)
}

func (s *S) TestStartStopServices(c *C) {
	s.startTestServices(c)

	if c.Failed() {
		return
	}

	s.stopTestServices(c)
}

func (s *S) TestStartStopServicesIdempotency(c *C) {
	s.startTestServices(c)
	if c.Failed() {
		return
	}

	s.startTestServices(c)
	if c.Failed() {
		return
	}

	s.stopTestServices(c)
	if c.Failed() {
		return
	}

	s.stopTestServicesAlreadyDead(c)
}

func (s *S) stopTestServices(c *C) {
	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 2)

	chg := s.stopServices(c, []string{"test1", "test2"}, 2)

	// Ensure processes are gone indeed.
	c.Assert(cmds, HasLen, 2)
	for name, cmd := range cmds {
		err := cmd.Process.Signal(syscall.Signal(0))
		if err == nil {
			c.Fatalf("Process for %q did not stop properly", name)
		} else {
			c.Check(err, ErrorMatches, ".*process already finished.*")
		}
	}

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
}

func (s *S) stopTestServicesAlreadyDead(c *C) {
	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 0)

	chg := s.stopServices(c, []string{"test1", "test2"}, 2)

	c.Assert(cmds, HasLen, 0)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
}

func (s *S) TestReplanServices(c *C) {
	s.startTestServices(c)

	if c.Failed() {
		return
	}

	layer := parseLayer(c, 0, "layer3", planLayer3)
	err := s.manager.CombineLayer(layer)
	c.Assert(err, IsNil)

	stops, starts, err := s.manager.Replan()
	c.Assert(err, IsNil)
	c.Check(stops, DeepEquals, []string{"test2"})
	c.Check(starts, DeepEquals, []string{"test1", "test2"})

	s.stopTestServices(c)
}

func (s *S) TestReplanUpdatesConfig(c *C) {
	s.startTestServices(c)
	defer s.stopTestServices(c)

	// Ensure the ServiceManager's config reflects the plan config.
	config := s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Assert(config.OnSuccess, Equals, plan.ActionUnset)
	c.Assert(config.Summary, Equals, "")
	command := config.Command

	// Add a layer and override a couple of values.
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: merge
        summary: A summary!
        on-success: ignore
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Call Replan and ensure the ServiceManager's config has updated.
	_, _, err = s.manager.Replan()
	c.Assert(err, IsNil)
	config = s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Check(config.OnSuccess, Equals, plan.ActionIgnore)
	c.Check(config.Summary, Equals, "A summary!")
	c.Check(config.Command, Equals, command)
}

func (s *S) TestStopStartUpdatesConfig(c *C) {
	s.startTestServices(c)
	defer s.stopTestServices(c)

	// Add a layer and override a couple of values.
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: merge
        summary: A summary!
        on-success: ignore
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Call Stop and Start and ensure the ServiceManager's config has updated.
	s.stopTestServices(c)
	s.startTestServices(c)
	config := s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Check(config.OnSuccess, Equals, plan.ActionIgnore)
	c.Check(config.Summary, Equals, "A summary!")
}

func (s *S) TestServiceLogs(c *C) {
	outputs := map[string]string{
		"test1": `2.* \[test1\] test1\n`,
		"test2": `2.* \[test2\] test2\n`,
	}
	s.testServiceLogs(c, outputs)

	// Run test again, but ensure the logs from the previous run are still in the ring buffer.
	outputs["test1"] += outputs["test1"]
	outputs["test2"] += outputs["test2"]
	s.testServiceLogs(c, outputs)
}

func (s *S) testServiceLogs(c *C, outputs map[string]string) {
	s.startTestServices(c)

	if c.Failed() {
		return
	}

	iterators, err := s.manager.ServiceLogs([]string{"test1", "test2"}, -1)
	c.Assert(err, IsNil)
	c.Assert(iterators, HasLen, 2)

	for serviceName, it := range iterators {
		buf := &bytes.Buffer{}
		for it.Next(nil) {
			_, err = io.Copy(buf, it)
			c.Assert(err, IsNil)
		}

		c.Assert(buf.String(), Matches, outputs[serviceName])

		err = it.Close()
		c.Assert(err, IsNil)
	}

	s.stopTestServices(c)
}

func (s *S) TestStartBadCommand(c *C) {
	chg := s.startServices(c, []string{"test3"}, 1)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot start.*"some-bad-command":.*not found.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test3")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)
}

func (s *S) TestUserGroupFails(c *C) {
	// Test with user and group will fail due to permission issues (unless
	// running as root)
	if os.Getuid() == 0 {
		c.Skip("requires non-root user")
	}

	var gotUid uint32
	var gotGid uint32
	restore := servstate.FakeSetCmdCredential(func(cmd *exec.Cmd, credential *syscall.Credential) {
		gotUid = credential.Uid
		gotGid = credential.Gid
		cmd.SysProcAttr.Credential = credential
	})
	defer restore()

	chg := s.startServices(c, []string{"test5"}, 1)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `.*\n.*cannot start service: .* operation not permitted.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test5")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)

	// Ensure that setCmdCredential was called with the correct UID and GID
	u, err := user.Lookup("nobody")
	c.Check(err, IsNil)
	uid, _ := strconv.Atoi(u.Uid)
	c.Check(gotUid, Equals, uint32(uid))
	g, err := user.LookupGroup("nogroup")
	c.Check(err, IsNil)
	gid, _ := strconv.Atoi(g.Gid)
	c.Check(gotGid, Equals, uint32(gid))
}

func (s *S) serviceByName(c *C, name string) *servstate.ServiceInfo {
	services, err := s.manager.Services([]string{name})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	return services[0]
}

func (s *S) TestStartFastExitCommand(c *C) {
	chg := s.startServices(c, []string{"test4"}, 1)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\n- Start service "test4" \(cannot start service: exited quickly with code 0\)`)
	c.Check(chg.Tasks()[0].Log(), HasLen, 2)
	c.Check(chg.Tasks()[0].Log()[0], Matches, `(?s).* INFO Most recent service output:\n    too-fast\n    second line`)
	c.Check(chg.Tasks()[0].Log()[1], Matches, `.* ERROR cannot start service: exited quickly with code 0`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test4")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)
}

func planYAML(c *C, manager *servstate.ServiceManager) string {
	plan, err := manager.Plan()
	c.Assert(err, IsNil)
	yml, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	return string(yml)
}

func (s *S) TestPlan(c *C) {
	expected := fmt.Sprintf(`
services:
    test1:
        startup: enabled
        override: replace
        command: /bin/sh -c "echo test1 | tee -a %s; sleep 300"
        before:
            - test2
        requires:
            - test2
    test2:
        override: replace
        command: /bin/sh -c "echo test2 | tee -a %s; sleep 300"
    test3:
        override: replace
        command: some-bad-command
    test4:
        override: replace
        command: echo -e 'too-fast\nsecond line'
    test5:
        override: replace
        command: /bin/sh -c "sleep 300"
        user: nobody
        group: nogroup
`[1:], s.log, s.log)
	c.Assert(planYAML(c, s.manager), Equals, expected)
}

func parseLayer(c *C, order int, label, layerYAML string) *plan.Layer {
	layer, err := plan.ParseLayer(order, label, []byte(layerYAML))
	c.Assert(err, IsNil)
	return layer
}

func (s *S) planLayersHasLen(c *C, manager *servstate.ServiceManager, expectedLen int) {
	plan, err := manager.Plan()
	c.Assert(err, IsNil)
	c.Assert(plan.Layers, HasLen, expectedLen)
}

func (s *S) TestAppendLayer(c *C) {
	dir := c.MkDir()
	os.Mkdir(filepath.Join(dir, "layers"), 0755)
	runner := state.NewTaskRunner(s.st)
	manager, err := servstate.NewManager(s.st, runner, dir, nil, nil)
	c.Assert(err, IsNil)

	// Append a layer when there are no layers.
	layer := parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
	err = manager.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	s.planLayersHasLen(c, manager, 1)

	// Try to append a layer when that label already exists.
	layer = parseLayer(c, 0, "label1", `
services:
    svc1:
        override: foobar
        command: /bin/bar
`)
	err = manager.AppendLayer(layer)
	c.Assert(err.(*servstate.LabelExists).Label, Equals, "label1")
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	s.planLayersHasLen(c, manager, 1)

	// Append another layer on top.
	layer = parseLayer(c, 0, "label2", `
services:
    svc1:
        override: replace
        command: /bin/bash
`)
	err = manager.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
`[1:])
	s.planLayersHasLen(c, manager, 2)

	// Append a layer with a different service.
	layer = parseLayer(c, 0, "label3", `
services:
    svc2:
        override: replace
        command: /bin/foo
`)
	err = manager.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	s.planLayersHasLen(c, manager, 3)
}

func (s *S) TestCombineLayer(c *C) {
	dir := c.MkDir()
	os.Mkdir(filepath.Join(dir, "layers"), 0755)
	runner := state.NewTaskRunner(s.st)
	manager, err := servstate.NewManager(s.st, runner, dir, nil, nil)
	c.Assert(err, IsNil)

	// "Combine" layer with no layers should just append.
	layer := parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
	err = manager.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	s.planLayersHasLen(c, manager, 1)

	// Combine layer with different label should just append.
	layer = parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/foo
`)
	err = manager.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	s.planLayersHasLen(c, manager, 2)

	// Combine layer with first layer.
	layer = parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/bash
`)
	err = manager.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	s.planLayersHasLen(c, manager, 2)

	// Combine layer with second layer.
	layer = parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/bar
`)
	err = manager.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/bar
`[1:])
	s.planLayersHasLen(c, manager, 2)

	// One last append for good measure.
	layer = parseLayer(c, 0, "label3", `
services:
    svc1:
        override: replace
        command: /bin/a
    svc2:
        override: replace
        command: /bin/b
`)
	err = manager.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/a
    svc2:
        override: replace
        command: /bin/b
`[1:])
	s.planLayersHasLen(c, manager, 3)
}

func (s *S) TestServices(c *C) {
	services, err := s.manager.Services(nil)
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test1", Current: servstate.StatusInactive, Startup: servstate.StartupEnabled},
		{Name: "test2", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test4", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test5", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})

	services, err = s.manager.Services([]string{"test2", "test3"})
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test2", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})

	// Start a service and ensure it's marked active
	s.startServices(c, []string{"test2"}, 1)

	services, err = s.manager.Services(nil)
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test1", Current: servstate.StatusInactive, Startup: servstate.StartupEnabled},
		{Name: "test2", Current: servstate.StatusActive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test4", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test5", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})
}

var planLayerEnv = `
services:
    envtest:
        override: replace
        command: /bin/sh -c "env | grep PEBBLE_ENV_TEST | sort > %s; sleep 300"
        environment:
            PEBBLE_ENV_TEST_1: foo
            PEBBLE_ENV_TEST_2: bar bazz
`

func (s *S) TestEnvironment(c *C) {
	// Setup new state and add "envtest" layer
	dir := c.MkDir()
	logPath := filepath.Join(dir, "log.txt")
	layerYAML := fmt.Sprintf(planLayerEnv, logPath)
	layer := parseLayer(c, 0, "envlayer", layerYAML)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Set environment variables in the current process to ensure we're
	// passing down the parent's environment too, but the layer's config
	// should override these if also set there.
	err = os.Setenv("PEBBLE_ENV_TEST_PARENT", "from-parent")
	c.Assert(err, IsNil)
	err = os.Setenv("PEBBLE_ENV_TEST_1", "should be overridden")
	c.Assert(err, IsNil)

	// Start "envtest" service
	chg := s.startServices(c, []string{"envtest"}, 1)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	// Ensure it read environment variables correctly
	data, err := ioutil.ReadFile(logPath)
	if os.IsNotExist(err) {
		c.Fatal("'envtest' service did not run")
	}
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
PEBBLE_ENV_TEST_1=foo
PEBBLE_ENV_TEST_2=bar bazz
PEBBLE_ENV_TEST_PARENT=from-parent
`[1:])
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func (s *S) TestActionRestart(c *C) {
	// Add custom backoff delay so it auto-restarts quickly.
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: merge
        command: /bin/sh -c "echo test2; exec sleep 300"
        backoff-delay: 50ms
        backoff-limit: 150ms
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up the first time.
	chg := s.startServices(c, []string{"test2"}, 1)
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	c.Assert(s.manager.BackoffNum("test2"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()
	time.Sleep(10 * time.Millisecond) // ensure it has enough time to write to the log
	c.Check(s.logBufferString(), Matches, `2.* \[test2\] test2\n`)

	// Send signal to process to terminate it early.
	err = s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait for it to go into backoff state.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff && s.manager.BackoffNum("test2") == 1
	})

	// Then wait for it to auto-restart (backoff time plus a bit).
	time.Sleep(75 * time.Millisecond)
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Check(s.logBufferString(), Matches, `2.* \[test2\] test2\n`)

	// Send signal to terminate it again.
	err = s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait for it to go into backoff state.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff && s.manager.BackoffNum("test2") == 2
	})

	// Then wait for it to auto-restart (backoff time plus a bit).
	time.Sleep(125 * time.Millisecond)
	svc = s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Check(s.logBufferString(), Matches, `2.* \[test2\] test2\n`)

	// Test that backoff reset time is working (set to backoff-limit)
	time.Sleep(175 * time.Millisecond)
	c.Check(s.manager.BackoffNum("test2"), Equals, 0)

	// Send signal to process to terminate it early.
	err = s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait for it to go into backoff state (back to backoff 1 again).
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff && s.manager.BackoffNum("test2") == 1
	})

	// Then wait for it to auto-restart (backoff time plus a bit).
	time.Sleep(75 * time.Millisecond)
	svc = s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Check(s.logBufferString(), Matches, `2.* \[test2\] test2\n`)
}

func (s *S) TestStopDuringBackoff(c *C) {
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: merge
        command: sleep 0.1
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up the first time.
	chg := s.startServices(c, []string{"test2"}, 1)
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	c.Assert(s.manager.BackoffNum("test2"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()

	// Wait for it to exit and go into backoff state.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff && s.manager.BackoffNum("test2") == 1
	})

	// Ensure it can be stopped successfully.
	chg = s.stopServices(c, []string{"test2"}, 1)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusInactive
	})
}

func (s *S) TestOnCheckFailureRestartWhileRunning(c *C) {
	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager()
	defer checkMgr.PlanChanged(&plan.Plan{})
	s.manager.NotifyPlanChanged(checkMgr.PlanChanged)

	// Tell service manager about check failures
	checkMgr.NotifyCheckFailed(s.manager.CheckFailed)

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := parseLayer(c, 0, "layer", fmt.Sprintf(`
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; sleep 1'
        backoff-delay: 50ms
        on-check-failure:
            chk1: restart

checks:
    chk1:
         override: replace
         period: 75ms  # a bit longer than shortOkayWait
         threshold: 1
         exec:
             command: will-fail
`, tempFile))
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, []string{"test2"}, 1)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := ioutil.ReadFile(tempFile)
		if string(b) == "x\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now wait till check happens (it will-fail)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for check to fail")
		}
		checks, err := checkMgr.Checks()
		c.Assert(err, IsNil)
		if len(checks) == 1 && checks[0].Status != checkstate.CheckStatusUp {
			c.Assert(checks[0].Failures, Equals, 1)
			c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Check failure should terminate process, backoff, and restart it, so wait for that
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, err := ioutil.ReadFile(tempFile)
		c.Assert(err, IsNil)
		if string(b) == "x\nx\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Shouldn't be restarted again
	time.Sleep(100 * time.Millisecond)
	b, err := ioutil.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")
	checks, err := checkMgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(len(checks), Equals, 1)
	c.Assert(checks[0].Status, Equals, checkstate.CheckStatusDown)
	c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Assert(s.manager.BackoffNum("test2"), Equals, 1)
}

func (s *S) TestOnCheckFailureRestartDuringBackoff(c *C) {
	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager()
	defer checkMgr.PlanChanged(&plan.Plan{})
	s.manager.NotifyPlanChanged(checkMgr.PlanChanged)

	// Tell service manager about check failures
	checkMgr.NotifyCheckFailed(s.manager.CheckFailed)

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := parseLayer(c, 0, "layer", fmt.Sprintf(`
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; sleep 0.075'
        backoff-delay: 50ms
        backoff-factor: 100  # ensure it only backoff-restarts once
        on-check-failure:
            chk1: restart

checks:
    chk1:
         override: replace
         period: 100ms
         threshold: 1
         exec:
             command: will-fail
`, tempFile))
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, []string{"test2"}, 1)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := ioutil.ReadFile(tempFile)
		if string(b) == "x\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Ensure it exits and goes into backoff state
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})

	// Check failure should wait for current backoff (after which it will be restarted)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, err := ioutil.ReadFile(tempFile)
		c.Assert(err, IsNil)
		if string(b) == "x\nx\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Assert(s.manager.BackoffNum("test2"), Equals, 1)

	// Shouldn't be restarted again
	time.Sleep(125 * time.Millisecond)
	b, err := ioutil.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")
	checks, err := checkMgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(len(checks), Equals, 1)
	c.Assert(checks[0].Status, Equals, checkstate.CheckStatusDown)
	c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
}

func (s *S) TestOnCheckFailureIgnore(c *C) {
	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager()
	defer checkMgr.PlanChanged(&plan.Plan{})
	s.manager.NotifyPlanChanged(checkMgr.PlanChanged)

	// Tell service manager about check failures
	checkMgr.NotifyCheckFailed(s.manager.CheckFailed)

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := parseLayer(c, 0, "layer", fmt.Sprintf(`
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; sleep 1'
        on-check-failure:
            chk1: ignore

checks:
    chk1:
         override: replace
         period: 75ms  # a bit longer than shortOkayWait
         threshold: 1
         exec:
             command: will-fail
`, tempFile))
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, []string{"test2"}, 1)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := ioutil.ReadFile(tempFile)
		if string(b) == "x\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now wait till check happens (it will-fail)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for check to fail")
		}
		checks, err := checkMgr.Checks()
		c.Assert(err, IsNil)
		if len(checks) == 1 && checks[0].Status != checkstate.CheckStatusUp {
			c.Assert(checks[0].Failures, Equals, 1)
			c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Service shouldn't have been restarted
	time.Sleep(100 * time.Millisecond)
	b, err := ioutil.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")
	checks, err := checkMgr.Checks()
	c.Assert(err, IsNil)
	c.Assert(len(checks), Equals, 1)
	c.Assert(checks[0].Status, Equals, checkstate.CheckStatusDown)
	c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
}

func (s *S) TestOnCheckFailureShutdown(c *C) {
	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager()
	defer checkMgr.PlanChanged(&plan.Plan{})
	s.manager.NotifyPlanChanged(checkMgr.PlanChanged)

	// Tell service manager about check failures
	checkMgr.NotifyCheckFailed(s.manager.CheckFailed)

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := parseLayer(c, 0, "layer", fmt.Sprintf(`
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; sleep 1'
        on-check-failure:
            chk1: shutdown

checks:
    chk1:
         override: replace
         period: 75ms  # a bit longer than shortOkayWait
         threshold: 1
         exec:
             command: will-fail
`, tempFile))
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, []string{"test2"}, 1)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for command to start")
		}
		b, _ := ioutil.ReadFile(tempFile)
		if string(b) == "x\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now wait till check happens (it will-fail)
	for i := 0; ; i++ {
		if i >= 100 {
			c.Fatalf("failed waiting for check to fail")
		}
		checks, err := checkMgr.Checks()
		c.Assert(err, IsNil)
		if len(checks) == 1 && checks[0].Status != checkstate.CheckStatusUp {
			c.Assert(checks[0].Failures, Equals, 1)
			c.Assert(checks[0].LastError, Matches, ".* executable file not found .*")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// It should have closed the stopDaemon channel.
	select {
	case <-s.stopDaemon:
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) waitUntilService(c *C, service string, f func(svc *servstate.ServiceInfo) bool) {
	for i := 0; i < 20; i++ {
		svc := s.serviceByName(c, service)
		if f(svc) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.Fatalf("timed out waiting for service")
}

func (s *S) TestActionShutdown(c *C) {
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: replace
        command: sleep 0.15
        on-success: shutdown
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up the first time.
	s.startServices(c, []string{"test2"}, 1)
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})

	// It should have closed the stopDaemon channel.
	select {
	case <-s.stopDaemon:
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestActionIgnore(c *C) {
	layer := parseLayer(c, 0, "layer", `
services:
    test2:
        override: replace
        command: sleep 0.15
        on-success: ignore
`)
	err := s.manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Start service and wait till it starts up the first time.
	s.startServices(c, []string{"test2"}, 1)
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})
}

func (s *S) TestGetAction(c *C) {
	tests := []struct {
		onSuccess plan.ServiceAction
		onFailure plan.ServiceAction
		success   bool
		action    string
		onType    string
	}{
		{onSuccess: "", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "", onFailure: "", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "", onFailure: "restart", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "", onFailure: "shutdown", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "", onFailure: "ignore", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "restart", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "shutdown", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "ignore", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "restart", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "shutdown", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "ignore", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "restart", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "shutdown", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "ignore", success: true, action: "ignore", onType: "on-success"},
	}
	for _, test := range tests {
		config := &plan.Service{
			OnFailure: test.onFailure,
			OnSuccess: test.onSuccess,
		}
		action, onType := servstate.GetAction(config, test.success)
		c.Check(string(action), Equals, test.action, Commentf("onSuccess=%q, onFailure=%q, success=%v",
			test.onSuccess, test.onFailure, test.success))
		c.Check(onType, Equals, test.onType, Commentf("onSuccess=%q, onFailure=%q, success=%v",
			test.onSuccess, test.onFailure, test.success))
	}
}

func (s *S) TestGetJitter(c *C) {
	// It's tricky to test a function that generates randomness, but ensure all
	// the values are in range, and that the number of values distributed across
	// each of 3 buckets is reasonable.
	var buckets [3]int
	for i := 0; i < 3000; i++ {
		jitter := s.manager.GetJitter(3 * time.Second)
		c.Assert(jitter >= 0 && jitter < 300*time.Millisecond, Equals, true)
		switch {
		case jitter >= 0 && jitter < 100*time.Millisecond:
			buckets[0]++
		case jitter >= 100*time.Millisecond && jitter < 200*time.Millisecond:
			buckets[1]++
		case jitter >= 200*time.Millisecond && jitter < 300*time.Millisecond:
			buckets[2]++
		default:
			c.Errorf("jitter %s outside range [0, 300ms)", jitter)
		}
	}
	for i := 0; i < 3; i++ {
		if buckets[i] < 800 || buckets[i] > 1200 { // exceedingly unlikely to be outside this range
			c.Errorf("bucket[%d] has too few or too many values in it (%d)", i, buckets[i])
		}
	}
}

func (s *S) TestCalculateNextBackoff(c *C) {
	tests := []struct {
		delay   time.Duration
		factor  float64
		limit   time.Duration
		current time.Duration
		next    time.Duration
	}{
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 0, next: 500 * time.Millisecond},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 500 * time.Millisecond, next: time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: time.Second, next: 2 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 16 * time.Second, next: 30 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 30 * time.Second, next: 30 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 1000 * time.Second, next: 30 * time.Second},

		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 0, next: time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: time.Second, next: 1500 * time.Millisecond},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 1500 * time.Millisecond, next: 2250 * time.Millisecond},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 50 * time.Second, next: 60 * time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 60 * time.Second, next: 60 * time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 70 * time.Second, next: 60 * time.Second},
	}
	for _, test := range tests {
		config := &plan.Service{
			BackoffDelay:  plan.OptionalDuration{Value: test.delay},
			BackoffFactor: plan.OptionalFloat{Value: test.factor},
			BackoffLimit:  plan.OptionalDuration{Value: test.limit},
		}
		next := servstate.CalculateNextBackoff(config, test.current)
		c.Check(next, Equals, test.next, Commentf("delay=%s, factor=%g, limit=%s, current=%s",
			test.delay, test.factor, test.limit, test.current))
	}
}

func (s *S) TestShutdown(c *C) {
	s.startServices(c, []string{"test2"}, 1)
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	getShutdownChange := func() *state.Change {
		s.st.Lock()
		defer s.st.Unlock()
		changes := s.st.Changes()
		for _, change := range changes {
			if change.Kind() == "shutdown" {
				return change
			}
		}
		return nil
	}

	// We need a goroutine to call ensure because Shutdown is synchronous.
	ensureDone := make(chan struct{})
	go func() {
		// "shutdown" change should appear almost immediately, but wait gracefully.
		for i := 0; ; i++ {
			if i >= 100 {
				c.Fatalf("timed out waiting for shutdown change")
			}
			change := getShutdownChange()
			if change != nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.ensure(c, 1)
		close(ensureDone)
	}()

	// Perform the shutdown.
	err := s.manager.Shutdown()
	c.Assert(err, IsNil)

	// Cleanly wait for ensure goroutine to finish.
	select {
	case <-ensureDone:
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for ensure goroutine to finish")
	}

	// Ensure that the service has been stopped.
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)

	// Ensure that it's created the expected "shutdown" change (with stop tasks).
	change := getShutdownChange()
	c.Assert(change, NotNil)
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(change.Summary(), Equals, "Shut down service manager")
	c.Check(change.Status(), Equals, state.DoneStatus)
	shutdownTasks := change.Tasks()
	c.Assert(shutdownTasks, HasLen, 1)
	c.Check(shutdownTasks[0].Kind(), Equals, "stop")
}
