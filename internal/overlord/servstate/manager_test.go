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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
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

	dir string
	log string

	st *state.State

	manager *servstate.ServiceManager
	runner  *state.TaskRunner
}

var _ = Suite(&S{})

var planLayer1 = `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo test1 >> %s; sleep 300"
        default: start
        requires:
            - test2
        before:
            - test2

    test2:
        override: replace
        command: /bin/sh -c "echo test2 >> %s; sleep 300"
`

var planLayer2 = `
services:
    test3:
        override: replace
        command: some-bad-command

    test4:
        override: replace
        command: echo too-fast
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

	s.runner = state.NewTaskRunner(s.st)
	manager, err := servstate.NewManager(s.st, s.runner, s.dir)
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
	data, err := ioutil.ReadFile(s.log)
	if os.IsNotExist(err) {
		c.Fatal("Services have not run")
	}
	c.Assert(err, IsNil)
	c.Assert(string(data), Matches, "(?s)"+expected)
}

func (s *S) TestDefaultServices(c *C) {
	services, err := s.manager.DefaultServices()
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []string{"test1", "test2"})
}

func (s *S) ensure(c *C, n int) {
	for i := 0; i < n; i++ {
		s.runner.Ensure()
		s.runner.Wait()
	}
}

func (s *S) TestStartStopServices(c *C) {

	// === Start ===

	services := []string{"test1", "test2"}

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

	s.assertLog(c, "test1\ntest2\n")

	cmds := s.manager.CmdsForTest()
	c.Check(cmds, HasLen, 2)

	if c.Failed() {
		return
	}

	// === ActiveServices ===

	// No implicit sorting for active services.
	active := s.manager.ActiveServices()
	sort.Strings(active)
	c.Check(services, DeepEquals, []string{"test1", "test2"})

	// === Stop ===

	s.st.Lock()
	// Stopping should happen in reverse order in practice. For now
	// it's up to the call site to organize that.
	ts, err = servstate.Stop(s.st, services)
	c.Check(err, IsNil)
	chg = s.st.NewChange("test", "Stop test")
	chg.AddAll(ts)
	s.st.Unlock()

	// Twice due to the cross-task dependency.
	s.ensure(c, 2)

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
        default: start
        override: replace
        command: /bin/sh -c "echo test1 >> %s; sleep 300"
        before:
            - test2
        requires:
            - test2
    test2:
        override: replace
        command: /bin/sh -c "echo test2 >> %s; sleep 300"
    test3:
        override: replace
        command: some-bad-command
    test4:
        override: replace
        command: echo too-fast
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
	manager, err := servstate.NewManager(s.st, runner, dir)
	c.Assert(err, IsNil)

	// Append a layer when there are no layers.
	layer := parseLayer(c, 0, "label", `
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

	// Try to append a layer with an invalid override.
	layer = parseLayer(c, 0, "label", `
services:
    svc1:
        override: foobar
        command: /bin/bar
`)
	err = manager.AppendLayer(layer)
	c.Assert(err, ErrorMatches, `layer "label" has invalid 'override' value .*`)
	c.Assert(planYAML(c, manager), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	s.planLayersHasLen(c, manager, 1)

	// Append another layer on top.
	layer = parseLayer(c, 0, "label", `
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
	layer = parseLayer(c, 0, "label", `
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
	manager, err := servstate.NewManager(s.st, runner, dir)
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
