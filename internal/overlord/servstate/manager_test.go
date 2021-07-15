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

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type S struct {
	testutil.BaseTest

	dir          string
	log          string
	logBuffer    bytes.Buffer
	logBufferMut sync.Mutex

	st *state.State

	manager *servstate.ServiceManager
	runner  *state.TaskRunner
}

var _ = Suite(&S{})

var planLayer1 = `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo test1 | tee -a $FILE; sleep 300"
        startup: enabled
        requires:
            - test2
        before:
            - test2
        environment:
            FILE: %s

    test2:
        override: replace
        command: /bin/sh -c "echo test2 | tee -a $FILE; sleep 300"
        environment:
            FILE: %s
`

var planLayer2 = `
services:
    test3:
        override: replace
        command: some-bad-command

    test4:
        override: replace
        command: echo too-fast

    test5:
        override: replace
        command: /bin/sh -c "sleep 300"
        user: nobody
        group: nogroup
`

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
	manager, err := servstate.NewManager(s.st, s.runner, s.dir, logOutput)
	c.Assert(err, IsNil)
	s.manager = manager

	restore := servstate.FakeOkayWait(100 * time.Millisecond)
	s.AddCleanup(restore)
	restore = servstate.FakeKillWait(100*time.Millisecond, 1000*time.Millisecond)
	s.AddCleanup(restore)
}

func (s *S) TearDownTest(c *C) {
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

func (s *S) startServices(c *C, services []string) {
	s.st.Lock()
	ts, err := servstate.Start(s.st, services)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Twice due to the cross-task dependency.
	s.ensure(c, 2)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	s.assertLog(c, ".*test1\n.*test2\n")

	cmds := s.manager.CmdsForTest()
	c.Check(cmds, HasLen, len(services))
}

func (s *S) TestStartStopServices(c *C) {
	services := []string{"test1", "test2"}
	s.startServices(c, services)

	if c.Failed() {
		return
	}

	s.stopServices(c, services)
}

func (s *S) TestStartStopServicesIdempotency(c *C) {
	services := []string{"test1", "test2"}
	s.startServices(c, services)
	if c.Failed() {
		return
	}

	s.startServices(c, services)
	if c.Failed() {
		return
	}

	s.stopServices(c, services)
	if c.Failed() {
		return
	}

	s.stopServicesAlreadyDead(c, services)
}

func (s *S) stopServices(c *C, services []string) {
	cmds := s.manager.CmdsForTest()
	c.Check(cmds, HasLen, len(services))

	s.st.Lock()
	// Stopping should happen in reverse order in practice. For now
	// it's up to the call site to organize that.
	ts, err := servstate.Stop(s.st, services)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Stop test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Twice due to the cross-task dependency.
	s.ensure(c, 2)

	// Ensure processes are gone indeed.
	c.Assert(cmds, HasLen, len(services))
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

func (s *S) stopServicesAlreadyDead(c *C, services []string) {
	cmds := s.manager.CmdsForTest()
	c.Check(cmds, HasLen, 0)

	s.st.Lock()
	// Stopping should happen in reverse order in practice. For now
	// it's up to the call site to organize that.
	ts, err := servstate.Stop(s.st, services)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Stop test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Twice due to the cross-task dependency.
	s.ensure(c, 2)

	c.Assert(cmds, HasLen, 0)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
}

func (s *S) TestServiceLogs(c *C) {
	services := []string{"test1", "test2"}
	outputs := map[string]string{
		"test1": `\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z \[test1\] test1\n`,
		"test2": `\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z \[test2\] test2\n`,
	}
	s.startServices(c, services)

	if c.Failed() {
		return
	}

	iterators, err := s.manager.ServiceLogs(services, -1)
	c.Assert(err, IsNil)
	c.Assert(iterators, HasLen, len(services))

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

	s.stopServices(c, services)
}

func (s *S) TestStartBadCommand(c *C) {
	s.st.Lock()
	ts, err := servstate.Start(s.st, []string{"test3"})
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()

	s.ensure(c, 1)

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

	s.st.Lock()
	ts, err := servstate.Start(s.st, []string{"test5"})
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()

	s.ensure(c, 1)

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
	servstate.FakeOkayWait(3000 * time.Millisecond)

	s.st.Lock()
	ts, err := servstate.Start(s.st, []string{"test4"})
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()

	s.ensure(c, 1)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot start.*exited quickly with code 0.*`)
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
        command: /bin/sh -c "echo test1 | tee -a $FILE; sleep 300"
        before:
            - test2
        requires:
            - test2
        environment:
            FILE: %s
    test2:
        override: replace
        command: /bin/sh -c "echo test2 | tee -a $FILE; sleep 300"
        environment:
            FILE: %s
    test3:
        override: replace
        command: some-bad-command
    test4:
        override: replace
        command: echo too-fast
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
	manager, err := servstate.NewManager(s.st, runner, dir, nil)
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
	manager, err := servstate.NewManager(s.st, runner, dir, nil)
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
	s.st.Lock()
	ts, err := servstate.Start(s.st, []string{"test2"})
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()
	s.ensure(c, 1)

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
        command: $PEBBLE_ENV_TEST_SHELL -c "env | grep PEBBLE_ENV_TEST | sort > %s; sleep 300"
        environment:
            PEBBLE_ENV_TEST_1: foo
            PEBBLE_ENV_TEST_2: bar bazz
`

func (s *S) TestEnvironment(c *C) {
	// Setup new state and add "envtest" layer
	st := state.New(nil)
	dir := c.MkDir()
	runner := state.NewTaskRunner(st)
	manager, err := servstate.NewManager(st, runner, dir, nil)
	c.Assert(err, IsNil)
	logPath := filepath.Join(dir, "log.txt")
	layerYAML := fmt.Sprintf(planLayerEnv, logPath)
	layer := parseLayer(c, 0, "envlayer", layerYAML)
	err = manager.AppendLayer(layer)
	c.Assert(err, IsNil)

	// Set environment variables in the current process to ensure we're
	// passing down the parent's environment too, but the layer's config
	// should override these if also set there.
	err = os.Setenv("PEBBLE_ENV_TEST_PARENT", "from-parent")
	c.Assert(err, IsNil)
	err = os.Setenv("PEBBLE_ENV_TEST_1", "should be overridden")
	c.Assert(err, IsNil)

	// Test command environment expansion.
	err = os.Setenv("PEBBLE_ENV_TEST_SHELL", "/bin/sh")
	c.Assert(err, IsNil)

	// Start "envtest" service
	st.Lock()
	ts, err := servstate.Start(st, []string{"envtest"})
	c.Check(err, IsNil)
	chg := st.NewChange("envtest", "Start envtest")
	chg.AddAll(ts)
	st.Unlock()
	runner.Ensure()
	runner.Wait()
	st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	st.Unlock()

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
PEBBLE_ENV_TEST_SHELL=/bin/sh
`[1:])
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}
