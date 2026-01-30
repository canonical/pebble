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
	"sync/atomic"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/workloads"
)

func (ps *planSuite) TestLoadInvalidPebbleDir(c *C) {
	var err error
	var numChanges atomic.Uint32

	ps.planMgr, err = planstate.NewManager("/invalid/path")
	c.Assert(err, IsNil)
	ps.planMgr.AddChangeListener(func(p *plan.Plan) {
		numChanges.Add(1)
	})
	// Load the plan from the <pebble-dir>/layers directory
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	plan := ps.planMgr.Plan()
	out, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	c.Assert(string(out), Equals, "{}\n")
	// A new, empty plan was created so change listeners must be called
	c.Assert(numChanges.Load(), Equals, uint32(1))
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	// Plan was already loaded, so no change listeners will be called
	c.Assert(numChanges.Load(), Equals, uint32(1))
}

func (ps *planSuite) TestInitInvalidPlan(c *C) {
	var err error
	var numChanges atomic.Uint32

	ps.planMgr, err = planstate.NewManager("/unused/path")
	c.Assert(err, IsNil)
	ps.planMgr.AddChangeListener(func(p *plan.Plan) {
		numChanges.Add(1)
	})
	// Attempt to initialize with an nil plan
	err = ps.planMgr.Init(nil)
	c.Assert(err, ErrorMatches, "cannot initialize plan manager with a nil plan")
	c.Assert(numChanges.Load(), Equals, uint32(0))
	// Attempt to initialize with an invalid plan
	p := &plan.Plan{
		Layers: []*plan.Layer{},
		Services: map[string]*plan.Service{
			// Test service with no command, which will make p.Validate() fail
			"test": {Command: ""},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.Section{},
	}
	err = ps.planMgr.Init(p)
	c.Assert(err, ErrorMatches, `plan must define "command" for service "test"`)
	c.Assert(numChanges.Load(), Equals, uint32(0))
}

func (ps *planSuite) TestInitOnce(c *C) {
	var err error
	var numChanges atomic.Uint32

	ps.planMgr, err = planstate.NewManager("/unused/path")
	c.Assert(err, IsNil)
	ps.planMgr.AddChangeListener(func(p *plan.Plan) {
		numChanges.Add(1)
	})
	// Attempt to initialize with an empty plan
	err = ps.planMgr.Init(plan.NewPlan())
	c.Assert(err, IsNil)
	c.Assert(numChanges.Load(), Equals, uint32(1))
	// Attempt to re-initialize, which will not take effect
	err = ps.planMgr.Init(nil)
	// Init() won't fail because the plan is already loaded
	c.Assert(err, IsNil)
	c.Assert(numChanges.Load(), Equals, uint32(1))
	// Attempt to re-initialize with a valid plan, which will also not take effect
	err = ps.planMgr.Init(plan.NewPlan())
	c.Assert(err, IsNil)
	c.Assert(numChanges.Load(), Equals, uint32(1))
}

var loadLayers = []string{`
	summary: Layer 1
	description: Layer 1 desc.
	services:
		svc1:
			summary: Svc1
			override: replace
			command: echo svc1
	test-field:
		test1:
			override: merge
			a: something
`, `
	summary: Layer 2
	description: Layer 2 desc.
	services:
		svc2:
			summary: Svc2
			override: replace
			command: echo svc2
	test-field:
		test1:
			override: merge
			b: something else
`}

func (ps *planSuite) TestLoadLayers(c *C) {
	plan.RegisterSectionExtension(testField, testExtension{})
	defer plan.UnregisterSectionExtension(testField)

	var err error
	var numChanges atomic.Uint32

	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)
	ps.planMgr.AddChangeListener(func(p *plan.Plan) {
		numChanges.Add(1)
	})
	// Write layers
	for _, l := range loadLayers {
		ps.writeLayer(c, string(reindent(l)))
	}
	// Load the plan from the <pebble-dir>/layers directory
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	c.Assert(numChanges.Load(), Equals, uint32(1))
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
test-field:
    test1:
        override: merge
        a: something
        b: something else
`[1:])
	// Attempt to reload should not take effect
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)
	c.Assert(numChanges.Load(), Equals, uint32(1))
}

func (ps *planSuite) TestAppendLayers(c *C) {
	plan.RegisterSectionExtension(testField, testExtension{})
	defer plan.UnregisterSectionExtension(testField)
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	// Append a layer when there are no layers.
	layer := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
test-field:
    test1:
        override: replace
        a: something
`)
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
test-field:
    test1:
        override: replace
        a: something
`[1:])
	ps.planLayersHasLen(c, 1)

	// Try to append a layer when that label already exists.
	layer = ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: foobar
        command: /bin/bar
test-field:
    test1:
        override: foobar
        a: something else
`)
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err.(*planstate.LabelExists).Label, Equals, "label1")
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
test-field:
    test1:
        override: replace
        a: something
`[1:])
	ps.planLayersHasLen(c, 1)

	// Append another layer on top.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc1:
        override: replace
        command: /bin/bash
test-field:
    test1:
        override: replace
        a: else
`)
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
test-field:
    test1:
        override: replace
        a: else
`[1:])
	ps.planLayersHasLen(c, 2)

	// Append a layer with a different service.
	layer = ps.parseLayer(c, 0, "label3", `
services:
    svc2:
        override: replace
        command: /bin/foo
test-field:
    test2:
        override: replace
        a: something
`)
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
test-field:
    test1:
        override: replace
        a: else
    test2:
        override: replace
        a: something
`[1:])
	ps.planLayersHasLen(c, 3)
}

func (ps *planSuite) TestCombineLayers(c *C) {
	plan.RegisterSectionExtension(testField, testExtension{})
	defer plan.UnregisterSectionExtension(testField)
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	// "Combine" layer with no layers should just append.
	layer := ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/sh
test-field:
    test1:
        override: replace
        a: something
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
test-field:
    test1:
        override: replace
        a: something
`[1:])
	ps.planLayersHasLen(c, 1)

	// Combine layer with different label should just append.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/foo
test-field:
    test2:
        override: replace
        a: else
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/sh
    svc2:
        override: replace
        command: /bin/foo
test-field:
    test1:
        override: replace
        a: something
    test2:
        override: replace
        a: else
`[1:])
	ps.planLayersHasLen(c, 2)

	// Combine layer with first layer.
	layer = ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: /bin/bash
test-field:
    test1:
        override: replace
        a: else
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 1000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/foo
test-field:
    test1:
        override: replace
        a: else
    test2:
        override: replace
        a: else
`[1:])
	ps.planLayersHasLen(c, 2)

	// Combine layer with second layer.
	layer = ps.parseLayer(c, 0, "label2", `
services:
    svc2:
        override: replace
        command: /bin/bar
test-field:
    test2:
        override: replace
        a: something
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 2000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/bash
    svc2:
        override: replace
        command: /bin/bar
test-field:
    test1:
        override: replace
        a: else
    test2:
        override: replace
        a: something
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
test-field:
    test1:
        override: replace
        a: nothing
    test2:
        override: replace
        a: nothing
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
	c.Assert(layer.Order, Equals, 3000)
	c.Assert(ps.planYAML(c), Equals, `
services:
    svc1:
        override: replace
        command: /bin/a
    svc2:
        override: replace
        command: /bin/b
test-field:
    test1:
        override: replace
        a: nothing
    test2:
        override: replace
        a: nothing
`[1:])
	ps.planLayersHasLen(c, 3)

	// Make sure that layer validation is happening.
	_, err = plan.ParseLayer(0, "label4", []byte(`
checks:
    bad-check:
        override: replace
        level: invalid
        tcp:
            port: 8080
`))
	c.Check(err, ErrorMatches, `(?s).*plan check.*must be "alive" or "ready".*`)

	// Make sure that layer validation is happening for extensions.
	_, err = plan.ParseLayer(0, "label4", []byte(`
test-field:
    my1:
        override: replace
        a: nothing
`))
	c.Check(err, ErrorMatches, `.*entry names must start with.*`)
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
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)

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
		calls++ // calls incremented here to satisfy staticcheck.
		planLock.Unlock()
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
		err = manager.AppendLayer(layer1, false)
		c.Assert(err, IsNil)

		err = manager.CombineLayer(layer1, false)
		c.Assert(err, IsNil)

		layer2 := ps.parseLayer(c, 0, "label2", `
services:
    svc1:
        override: replace
        command: /bin/sh
`)
		err = manager.CombineLayer(layer2, false)
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

func (ps *planSuite) TestAppendLayersWithoutInner(c *C) {
	plan.RegisterSectionExtension(testField, testExtension{})
	defer plan.UnregisterSectionExtension(testField)
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	layer := ps.parseLayer(c, 0, "foo/bar", "")
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	layer = ps.parseLayer(c, 0, "baz", "")
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
	layer = ps.parseLayer(c, 0, "foo/baz", "")
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, ErrorMatches, ".*cannot insert sub-directory.*")
}

func (ps *planSuite) TestAppendLayersWithInner(c *C) {
	plan.RegisterSectionExtension(testField, testExtension{})
	defer plan.UnregisterSectionExtension(testField)
	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)

	appendLabels := []string{
		"foo",
		"baz/aaa",
		"something",
		"baz/bbb",
		"else",
		"baz/ccc",
		"zzz/zzz",
		"final",
	}

	for _, label := range appendLabels {
		layer := ps.parseLayer(c, 0, label, "")
		err = ps.planMgr.AppendLayer(layer, true)
		c.Assert(err, IsNil)
	}

	layersResult := []struct {
		label string
		order int
	}{{
		label: "foo",
		order: 1000,
	}, {
		label: "baz/aaa",
		order: 2001,
	}, {
		label: "baz/bbb",
		order: 2002,
	}, {
		label: "baz/ccc",
		order: 2003,
	}, {
		label: "something",
		order: 3000,
	}, {
		label: "else",
		order: 4000,
	}, {
		label: "zzz/zzz",
		order: 5001,
	}, {
		label: "final",
		order: 6000,
	}}

	plan := ps.planMgr.Plan()
	// Check the layers order and each layer's order is correct.
	for i, layer := range layersResult {
		c.Assert(plan.Layers[i].Order, Equals, layer.order)
		c.Assert(plan.Layers[i].Label, Equals, layer.label)
	}
}

func (ps *planSuite) TestAppendWorkloadLayer(c *C) {
	plan.RegisterSectionExtension(workloads.WorkloadsField, &workloads.WorkloadsSectionExtension{})
	defer plan.UnregisterSectionExtension(workloads.WorkloadsField)

	ps.writeLayer(c, `
workloads:
    workload1:
        override: replace
`)

	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)

	// An attempt to mutate layers must fail
	layer := ps.parseLayer(c, 0, "workload2", `
workloads:
    workload2:
        override: replace
`)
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, ErrorMatches, `cannot change immutable section "workloads"`)

	// We are adding a new layer but we are not mutating existing workloads
	layer = ps.parseLayer(c, 0, "workloads", "workloads: {}")
	err = ps.planMgr.AppendLayer(layer, false)
	c.Assert(err, IsNil)
}

func (ps *planSuite) TestCombineWorkloadLayer(c *C) {
	plan.RegisterSectionExtension(workloads.WorkloadsField, &workloads.WorkloadsSectionExtension{})
	defer plan.UnregisterSectionExtension(workloads.WorkloadsField)

	ps.writeLayer(c, `
workloads:
    workload1:
        override: replace
`)

	var err error
	ps.planMgr, err = planstate.NewManager(ps.layersDir)
	c.Assert(err, IsNil)
	err = ps.planMgr.Load()
	c.Assert(err, IsNil)

	// An attempt to mutate layers must fail
	layer := ps.parseLayer(c, 0, "workload2", `
workloads:
    workload2:
        override: replace
`)
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, ErrorMatches, `cannot change immutable section "workloads"`)

	// We are adding a new layer but we are not mutating existing workloads
	layer = ps.parseLayer(c, 0, "workloads", "workloads: {}")
	err = ps.planMgr.CombineLayer(layer, false)
	c.Assert(err, IsNil)
}
