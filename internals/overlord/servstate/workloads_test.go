// Copyright (c) 2025 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package servstate_test

import (
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

var schemaTests = []struct {
	summary         string
	layers          []string
	combinedSection *servstate.WorkloadsSection
	combinedYAML    string
	error           string
}{{
	summary:         "empty section",
	combinedSection: &servstate.WorkloadsSection{},
	combinedYAML:    `workloads: {}`,
}, {
	summary: "single null workload",
	layers: []string{`
workloads:
    default:
    `},
	error: `workload "default" has a null value`,
}, {
	summary: "single workload with no override policy",
	layers: []string{`
workloads:
    no-override-policy:
        user-id: 1337
    `},
	error: `workload "no-override-policy" must define an "override" policy`,
}, {
	summary: "single workload with invalid override policy",
	layers: []string{`
workloads:
    invalid-override-policy:
        override: bazinga
    `},
	error: `workload "invalid-override-policy" has an invalid "override" policy: "bazinga"`,
}, {
	summary: "single workload with empty name",
	layers: []string{`
workloads:
    "":
        override: replace
    `},
	error: `workload "" cannot have an empty name`,
}, {
	summary: "single default workload",
	layers: []string{`
workloads:
    default:
        override: replace
    `},
	combinedSection: &servstate.WorkloadsSection{
		Entries: map[string]*servstate.Workload{
			"default": {
				Name:     "default",
				Override: plan.ReplaceOverride,
			},
		},
	},
	combinedYAML: `
workloads:
    default:
        override: replace
    `,
}, {
	summary: "single non-default workload",
	layers: []string{`
workloads:
    default:
        override: merge
        environment:
            foo: bar
            bar: baz
        user-id: 1337
        user: alyssa
        group-id: 1337
        group: hackers
    `},
	combinedSection: &servstate.WorkloadsSection{
		Entries: map[string]*servstate.Workload{
			"default": {
				Name:     "default",
				Override: plan.MergeOverride,
				Environment: map[string]string{
					"foo": "bar",
					"bar": "baz",
				},
				UserID:  makeptr(1337),
				User:    "alyssa",
				GroupID: makeptr(1337),
				Group:   "hackers",
			},
		},
	},
	combinedYAML: `
workloads:
    default:
        override: merge
        environment:
            bar: baz
            foo: bar
        user-id: 1337
        user: alyssa
        group-id: 1337
        group: hackers
    `,
}, {
	summary: "override replace policy",
	layers: []string{`
workloads:
    default:
        override: replace
        environment:
            a: b
            c: d
            e: f
        user-id: 25519
        user: ellie
        group-id: 25519
        group: curves
    `, `
workloads:
    default:
        override: replace
        environment:
            1: 2
            3: 4
            5: 6
        user-id: 256
        user: wilson
        group-id: 256
        group: wilson
    `,
	},
	combinedSection: &servstate.WorkloadsSection{
		Entries: map[string]*servstate.Workload{
			"default": {
				Name:     "default",
				Override: plan.ReplaceOverride,
				Environment: map[string]string{
					"1": "2",
					"3": "4",
					"5": "6",
				},
				UserID:  makeptr(256),
				User:    "wilson",
				GroupID: makeptr(256),
				Group:   "wilson",
			},
		},
	},
	combinedYAML: `
workloads:
    default:
        override: replace
        environment:
            "1": "2"
            "3": "4"
            "5": "6"
        user-id: 256
        user: wilson
        group-id: 256
        group: wilson
    `,
}, {
	summary: "merge override policy",
	layers: []string{`
workloads:
    default:
        override: merge
        environment:
            a: b
            c: d
            e: f
        user-id: 1000
        user: ubuntu
        group-id: 1000
        group: ubuntu
    `, `
workloads:
    default:
        override: merge
        environment:
            "1": "2"
            "3": "4"
            "5": "6"
        user-id: 1001
        user: live
        group-id: 1001
        group: live
    `},
	combinedSection: &servstate.WorkloadsSection{
		Entries: map[string]*servstate.Workload{
			"default": {
				Name:     "default",
				Override: plan.MergeOverride,
				Environment: map[string]string{
					"a": "b",
					"1": "2",
					"c": "d",
					"3": "4",
					"e": "f",
					"5": "6",
				},
				UserID:  makeptr(1001),
				User:    "live",
				GroupID: makeptr(1001),
				Group:   "live",
			},
		},
	},
	combinedYAML: `
workloads:
    default:
        override: merge
        environment:
            "1": "2"
            "3": "4"
            "5": "6"
            a: b
            c: d
            e: f
        user-id: 1001
        user: live
        group-id: 1001
        group: live
    `,
}}

func parseCombineLayers(yamls []string) (*plan.Layer, error) {
	var layers []*plan.Layer
	for i, yaml := range yamls {
		layer, err := plan.ParseLayer(i, fmt.Sprintf("test-plan-layer-%v", i), []byte(yaml))
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return plan.CombineLayers(layers...)
}

func layerYAML(c *C, layer *plan.Layer) string {
	yml, err := yaml.Marshal(layer)
	c.Assert(err, IsNil)
	return strings.TrimSpace(string(yml))
}

func (s *S) TestWorkloadsSectionExtensionSchema(c *C) {
	plan.RegisterSectionExtension(servstate.WorkloadsField, &servstate.WorkloadsSectionExtension{})
	defer plan.UnregisterSectionExtension(servstate.WorkloadsField)

	for i, t := range schemaTests {
		c.Logf("Running TestWorkloadsSectionExtensionSchema %q test using test data index %d\n", t.summary, i)
		combined, err := parseCombineLayers(t.layers)
		if t.error != "" {
			c.Assert(err, ErrorMatches, t.error)
		} else {
			c.Assert(err, IsNil)
			section, ok := combined.Sections[servstate.WorkloadsField]
			c.Assert(ok, Equals, true)
			c.Assert(section, NotNil)
			ws, ok := section.(*servstate.WorkloadsSection)
			c.Assert(ok, Equals, true)
			c.Assert(ws, DeepEquals, t.combinedSection)
			c.Assert(layerYAML(c, combined), Equals, strings.TrimSpace(t.combinedYAML))
		}

	}
}

func (s *S) TestWorkloadAppliesToService(c *C) {
	plan.RegisterSectionExtension(servstate.WorkloadsField, &servstate.WorkloadsSectionExtension{})
	defer plan.UnregisterSectionExtension(servstate.WorkloadsField)

	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo $PATH; sleep 10"
        workload: wl1

workloads:
    wl1:
        override: replace
        environment:
            PATH: "/private/bin:/bin:/sbin"
    `)
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	c.Assert(s.manager.BackoffNum("test1"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()
	time.Sleep(10 * time.Millisecond)
	c.Check(s.readAndClearLogBuffer(), Matches, `(?s).* \[test1\] /private/bin:/bin:/sbin\n`)
}

func (s *S) TestWorkloadReferenceInvalid(c *C) {

	plan.RegisterSectionExtension(servstate.WorkloadsField, &servstate.WorkloadsSectionExtension{})
	defer plan.UnregisterSectionExtension(servstate.WorkloadsField)

	s.newServiceManager(c)
	err := s.tryPlanAddLayer(c, `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo $PATH; sleep 10"
        workload: non-existing
    `)
	c.Assert(err, ErrorMatches, `plan service "test1" cannot run in unknown workload "non-existing"`)
}

func (s *S) tryPlanAddLayer(c *C, layerYAML string) error {
	cnt := len(s.plan.Layers)
	layer, err := plan.ParseLayer(cnt, fmt.Sprintf("test-plan-layer-%v", cnt), []byte(layerYAML))
	if err != nil {
		return err
	}
	// Resolve {{.NotifyDoneCheck}}
	s.insertDoneChecks(c, layer)
	layers := append(s.plan.Layers, layer)
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return err
	}
	if err := combined.Validate(); err != nil {
		return err
	}
	s.plan = &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
		Sections:   combined.Sections,
	}
	return s.plan.Validate()
}

func makeptr[T any](v T) *T {
	return &v
}
