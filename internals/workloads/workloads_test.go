// Copyright (c) 2025 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a Copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workloads_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/workloads"
)

var schemaTests = []struct {
	summary         string
	layers          []string
	combinedSection *workloads.WorkloadsSection
	combinedYAML    string
	error           string
}{{
	summary:         "empty section",
	combinedSection: &workloads.WorkloadsSection{},
	combinedYAML:    `workloads: {}`,
}, {
	summary: "single null workload",
	layers: []string{`
workloads:
    default:
    `},
	error: `workload "default" cannot have a null value`,
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
	combinedSection: &workloads.WorkloadsSection{
		Entries: map[string]*workloads.Workload{
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
        group-id: 1338
        group: hackers
    `},
	combinedSection: &workloads.WorkloadsSection{
		Entries: map[string]*workloads.Workload{
			"default": {
				Name:     "default",
				Override: plan.MergeOverride,
				Environment: map[string]string{
					"foo": "bar",
					"bar": "baz",
				},
				UserID:  ptr(1337),
				User:    "alyssa",
				GroupID: ptr(1338),
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
        group-id: 1338
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
        group-id: 41417
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
        group-id: 257
        group: users
    `,
	},
	combinedSection: &workloads.WorkloadsSection{
		Entries: map[string]*workloads.Workload{
			"default": {
				Name:     "default",
				Override: plan.ReplaceOverride,
				Environment: map[string]string{
					"1": "2",
					"3": "4",
					"5": "6",
				},
				UserID:  ptr(256),
				User:    "wilson",
				GroupID: ptr(257),
				Group:   "users",
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
        group-id: 257
        group: users
    `,
}, {
	summary: "Merge override policy",
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
        group-id: 1001
        group: linux
    `, `
workloads:
    default:
        override: merge
        environment:
            "1": "2"
            "3": "4"
            a: z
            "5": "6"
        user-id: 1001
        user: live
        group-id: 1002
        group: users
    `},
	combinedSection: &workloads.WorkloadsSection{
		Entries: map[string]*workloads.Workload{
			"default": {
				Name:     "default",
				Override: plan.MergeOverride,
				Environment: map[string]string{
					"a": "z",
					"1": "2",
					"c": "d",
					"3": "4",
					"e": "f",
					"5": "6",
				},
				UserID:  ptr(1001),
				User:    "live",
				GroupID: ptr(1002),
				Group:   "users",
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
            a: z
            c: d
            e: f
        user-id: 1001
        user: live
        group-id: 1002
        group: users
    `,
}}

func (s *workloadsSuite) TestWorkloadsSectionExtensionSchema(c *C) {
	plan.RegisterSectionExtension(workloads.WorkloadsField, &workloads.SectionExtension{})
	defer plan.UnregisterSectionExtension(workloads.WorkloadsField)

	for i, t := range schemaTests {
		c.Logf("Running TestWorkloadsSectionExtensionSchema %q test using test data index %d\n", t.summary, i)
		combined, err := parseCombineLayers(t.layers)
		if t.error != "" {
			c.Assert(err, ErrorMatches, t.error)
		} else {
			c.Assert(err, IsNil)
			section, ok := combined.Sections[workloads.WorkloadsField]
			c.Assert(ok, Equals, true)
			c.Assert(section, NotNil)
			ws, ok := section.(*workloads.WorkloadsSection)
			c.Assert(ok, Equals, true)
			c.Assert(ws, DeepEquals, t.combinedSection)
			c.Assert(layerYAML(c, combined), Equals, strings.TrimSpace(t.combinedYAML))
		}

	}
}

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

func ptr[T any](v T) *T {
	return &v
}
