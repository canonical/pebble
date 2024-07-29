// Copyright (c) 2024 Canonical Ltd
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

package planstate_test

import (
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
)

func (ps *planSuite) TestLoadInvalidPebbleDir(c *C) {
	var err error
	ps.planMgr, err = planstate.NewManager("/invalid/path")
	c.Assert(err, IsNil)
	// Load the plan from the <pebble-dir>/layers directory
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	plan := ps.planMgr.Plan()
	out, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	c.Assert(string(out), Equals, "{}\n")
}

var loadLayers = []string{`
	summary: Layer 1
	description: Layer 1 desc.
	services:
		svc1:
			summary: Svc1
			override: replace
			command: echo svc1
`, `
	summary: Layer 2
	description: Layer 2 desc.
	services:
		svc2:
			summary: Svc2
			override: replace
			command: echo svc2
`}

func (ps *planSuite) TestLoadLayers(c *C) {
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)
	// Write layers
	for _, l := range loadLayers {
		ps.writeLayer(c, string(reindent(l)))
	}
	// Load the plan from the <pebble-dir>/layers directory
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	plan := ps.planMgr.Plan()
	out, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	c.Assert(len(plan.Layers), Equals, 2)
	c.Assert(string(out), Equals, `
services:
    svc1:
        summary: Svc1
        override: replace
        command: echo svc1
    svc2:
        summary: Svc2
        override: replace
        command: echo svc2
`[1:])
}

func (ps *planSuite) TestAppendLayers(c *C) {
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	// Append a layer when there are no layers.
	layer := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
	err = ps.planMgr.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	ps.planLayersHasLen(c, 1)

	// Try to append a layer when that label already exists.
	layer = ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: foobar
        command: /bin/bar
`)
	err = ps.planMgr.AppendLayer(layer)
	c.Assert(err.(*planstate.LabelExists).Label, Equals, "label1")
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	ps.planLayersHasLen(c, 1)

	// Append another layer on top.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc1:
        override: replace
        command: /bin/bash
`)
	err = ps.planMgr.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
`[1:])
	ps.planLayersHasLen(c, 2)

	// Append a layer with a different service.
	layer = ps.parseLayer(c, 0, "label3", `
services:
    svc2:
        override: replace
        command: /bin/foo
`)
	err = ps.planMgr.AppendLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	ps.planLayersHasLen(c, 3)
}

func (ps *planSuite) TestCombineLayers(c *C) {
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	// "Combine" layer with no layers should just append.
	layer := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
	err = ps.planMgr.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
`[1:])
	ps.planLayersHasLen(c, 1)

	// Combine layer with different label should just append.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/foo
`)
	err = ps.planMgr.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	ps.planLayersHasLen(c, 2)

	// Combine layer with first layer.
	layer = ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/bash
`)
	err = ps.planMgr.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
`[1:])
	ps.planLayersHasLen(c, 2)

	// Combine layer with second layer.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/bar
`)
	err = ps.planMgr.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/bar
`[1:])
	ps.planLayersHasLen(c, 2)

	// One last append for good measure.
	layer = ps.parseLayer(c, 0, "label3", `
services:
    svc1:
        override: replace
        command: /bin/a
    svc2:
        override: replace
        command: /bin/b
`)
	err = ps.planMgr.CombineLayer(layer)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/a
    svc2:
        override: replace
        command: /bin/b
`[1:])
	ps.planLayersHasLen(c, 3)

	// Make sure that layer validation is happening.
	layer, err = plan.ParseLayer(0, "label4", []byte(`
checks:
    bad-check:
        override: replace
        level: invalid
        tcp:
            port: 8080
`))
	c.Check(err, ErrorMatches, `(?s).*plan check.*must be "alive" or "ready".*`)
}

func (ps *planSuite) TestSetServiceArgs(c *C) {
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	// This is the original plan
	layer := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: foo [ --bar ]
    svc2:
        override: replace
        command: foo
    svc3:
        override: replace
        command: foo
`)
	err = ps.planMgr.AppendLayer(layer)

	// Set arguments to services.
	serviceArgs := map[string][]string{
		"svc1": {"-abc", "--xyz"},
		"svc2": {"--bar"},
	}
	err = ps.planMgr.SetServiceArgs(serviceArgs)
	c.Assert(err, IsNil)

	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: foo [ -abc --xyz ]
    svc2:
        override: replace
        command: foo [ --bar ]
    svc3:
        override: replace
        command: foo
`[1:])
}

func (ps *planSuite) TestChangeListenerAndLocking(c *C) {
	manager, err := planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	calls := 0
	manager.AddChangeListener(func(p *plan.Plan) {
		// Plan lock shouldn't be held when calling change listener,
		// so we should be able to acquire it.
		planLock := manager.PlanLock()
		planLock.Lock()
		planLock.Unlock()
		calls++
	})

	// Run operations in goroutine so we can time out the test if it fails.
	done := make(chan struct{})
	go func() {
		ps.writeLayer(c, `
services:
    svc1:
        override: replace
        command: echo svc1
`)
		err = manager.Load()
		c.Assert(err, IsNil)

		layer1 := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
		err = manager.AppendLayer(layer1)
		c.Assert(err, IsNil)

		err = manager.CombineLayer(layer1)
		c.Assert(err, IsNil)

		layer2 := ps.parseLayer(c, 0, "label2", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
		err = manager.CombineLayer(layer2)
		c.Assert(err, IsNil)

		err = manager.SetServiceArgs(map[string][]string{
			"svc1": {"-abc", "--xyz"},
		})
		c.Assert(err, IsNil)

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.Fatal("timed out - plan operations must be holding the plan lock while calling the change listeners")
	}

	c.Assert(calls, Equals, 5)
}
