// Copyright (c) 2024 Canonical Ltd
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

package plan_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
)

type inputLayer struct {
	order int
	label string
	yaml  string
}

// PlanResult represents the final content of a combined plan. Since this
// test file exclusively focuses on extensions, all built-in sections are
// empty and ignored in the test results.
type planResult struct {
	x *xSection
	y *ySection
}

type extension struct {
	field string
	ext   plan.SectionExtension
}

var extensionTests = []struct {
	summary    string
	extensions []extension
	layers     []*inputLayer
	error      string
	result     *planResult
	resultYaml string
}{{
	summary:    "No Sections",
	resultYaml: "{}\n",
}, {
	summary: "Section using built-in name",
	extensions: []extension{{
		field: "summary",
		ext:   &xExtension{},
	}},
	error: ".*already used as built-in field.*",
}, {
	summary: "Sections with empty YAML",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	result: &planResult{
		x: &xSection{},
		y: &ySection{},
	},
	resultYaml: "{}\n",
}, {
	summary: "Load file layers invalid section",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-xy",
		yaml: `
			summary: xy
			description: desc
			invalid:`,
	}},
	error: "cannot parse layer .*: unknown section .*",
}, {
	summary: "Load file layers not unique order",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-1",
		yaml: `
			summary: xy
			description: desc`,
	}, {
		order: 1,
		label: "layer-2",
		yaml: `
			summary: xy
			description: desc`,
	}},
	error: "invalid layer filename: .* not unique .*",
}, {
	summary: "Load file layers not unique label",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-xy",
		yaml: `
			summary: xy
			description: desc`,
	}, {
		order: 2,
		label: "layer-xy",
		yaml: `
			summary: xy
			description: desc`,
	}},
	error: "invalid layer filename: .* not unique .*",
}, {
	summary: "Load file layers with empty section",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x`,
	}, {
		order: 2,
		label: "layer-y",
		yaml: `
			summary: y
			description: desc-y`,
	}},
	result: &planResult{
		x: &xSection{},
		y: &ySection{},
	},
	resultYaml: "{}\n",
}, {
	summary: "Load file layers with section validation failure #1",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x
			x-field:
				z1:
					override: replace
					a: a
					b: b`,
	}},
	error: ".*cannot accept entry not starting.*",
}, {
	summary: "Load file layers with section validation failure #2",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x
			x-field:
				x1:`,
	}},
	error: ".*cannot have nil entry.*",
}, {
	summary: "Load file layers failed plan validation",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x
			x-field:
				x1:
					override: replace
					a: a
					b: b
					y-field:
					  - y2`,
	}, {
		order: 2,
		label: "layer-y",
		yaml: `
			summary: y
			description: desc-y
			y-field:
				y1:
					override: replace
					a: a
					b: b`,
	}},
	error: ".*cannot find.*",
}, {
	summary: "Check empty section omits entry",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x
			x-field:`,
	}, {
		order: 2,
		label: "layer-y",
		yaml: `
			summary: y
			description: desc-y
			y-field:`,
	}},
	result: &planResult{
		x: &xSection{},
		y: &ySection{},
	},
	resultYaml: "{}\n",
}, {
	summary: "Load file layers",
	extensions: []extension{{
		field: "x-field",
		ext:   &xExtension{},
	}, {
		field: "y-field",
		ext:   &yExtension{},
	}},
	layers: []*inputLayer{{
		order: 1,
		label: "layer-x",
		yaml: `
			summary: x
			description: desc-x
			x-field:
				x1:
					override: replace
					a: a
					b: b
					y-field:
					  - y1`,
	}, {
		order: 2,
		label: "layer-y",
		yaml: `
			summary: y
			description: desc-y
			y-field:
				y1:
					override: replace
					a: a
					b: b`,
	}},
	result: &planResult{
		x: &xSection{
			Entries: map[string]*X{
				"x1": &X{
					Name:     "x1",
					Override: plan.ReplaceOverride,
					A:        "a",
					B:        "b",
					Y: []string{
						"y1",
					},
				},
			},
		},
		y: &ySection{
			Entries: map[string]*Y{
				"y1": &Y{
					Name:     "y1",
					Override: plan.ReplaceOverride,
					A:        "a",
					B:        "b",
				},
			},
		},
	},
	resultYaml: string(reindent(`
		x-field:
			x1:
				override: replace
				a: a
				b: b
				y-field:
					- y1
		y-field:
			y1:
				override: replace
				a: a
				b: b`)),
}}

func (s *S) TestPlanExtensions(c *C) {
	registeredExtensions := []string{}
	defer func() {
		// Remove remaining registered extensions.
		for _, field := range registeredExtensions {
			plan.UnregisterSectionExtension(field)
		}
	}()

nexttest:
	for testIndex, testData := range extensionTests {
		c.Logf("TestPlanExtensions :: %s (data index %v)", testData.summary, testIndex)

		// Unregister extensions from previous test iteraton.
		for _, field := range registeredExtensions {
			plan.UnregisterSectionExtension(field)
		}
		registeredExtensions = []string{}

		// Write layers to test directory.
		layersDir := filepath.Join(c.MkDir(), "layers")
		s.writeLayerFiles(c, layersDir, testData.layers)
		var p *plan.Plan

		// Register extensions for this test iteration.
		for _, e := range testData.extensions {
			err := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("%v", r)
					}
				}()
				plan.RegisterSectionExtension(e.field, e.ext)
				registeredExtensions = append(registeredExtensions, e.field)
				return nil
			}()
			if err != nil {
				c.Assert(err, ErrorMatches, testData.error)
				continue nexttest
			}
		}

		// Load the plan layer from disk (parse, combine and validate).
		p, err := plan.ReadDir(layersDir)
		if testData.error != "" || err != nil {
			// Expected error.
			c.Assert(err, ErrorMatches, testData.error)
			continue nexttest
		}

		if slices.ContainsFunc(testData.extensions, func(n extension) bool {
			return n.field == xField
		}) {
			// Verify "x-field" data.
			var x *xSection
			x = p.Sections[xField].(*xSection)
			c.Assert(err, IsNil)
			c.Assert(x.Entries, DeepEquals, testData.result.x.Entries)
		}

		if slices.ContainsFunc(testData.extensions, func(n extension) bool {
			return n.field == yField
		}) {
			// Verify "y-field" data.
			var y *ySection
			y = p.Sections[yField].(*ySection)
			c.Assert(err, IsNil)
			c.Assert(y.Entries, DeepEquals, testData.result.y.Entries)
		}

		// Verify combined plan YAML.
		planYAML, err := yaml.Marshal(p)
		c.Assert(err, IsNil)
		c.Assert(string(planYAML), Equals, testData.resultYaml)
	}
}

// TestSectionOrderExt ensures built-in and extension section ordering
// rules are maintained. Extensions are ordered according to the order of
// registration and follows the built-in sections which are ordered
// the same way they are defined in the Plan struct.
func (s *S) TestSectionOrderExt(c *C) {
	plan.RegisterSectionExtension("x-field", &xExtension{})
	plan.RegisterSectionExtension("y-field", &yExtension{})
	defer func() {
		plan.UnregisterSectionExtension("x-field")
		plan.UnregisterSectionExtension("y-field")
	}()

	layer, err := plan.ParseLayer(1, "label", reindent(`
		y-field:
			y1:
				override: replace
				a: a
				b: b
		checks:
			chk1:
				override: replace
				exec:
					command: ping 8.8.8.8
		x-field:
			x1:
				override: replace
				a: a
				b: b
				y-field:
					- y1
		log-targets:
			lt1:
				override: replace
				type: loki
				location: http://192.168.1.2:3100/loki/api/v1/push
		services:
			srv1:
				override: replace
				command: cmd`))
	c.Assert(err, IsNil)
	combined, err := plan.CombineLayers(layer)
	c.Assert(err, IsNil)
	plan := plan.Plan{
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
		Sections:   combined.Sections,
	}
	data, err := yaml.Marshal(plan)
	c.Assert(string(data), Equals, string(reindent(`
		services:
			srv1:
				override: replace
				command: cmd
		checks:
			chk1:
				override: replace
				threshold: 3
				exec:
					command: ping 8.8.8.8
		log-targets:
			lt1:
				type: loki
				location: http://192.168.1.2:3100/loki/api/v1/push
				services: []
				override: replace
		x-field:
			x1:
				override: replace
				a: a
				b: b
				y-field:
					- y1
		y-field:
			y1:
				override: replace
				a: a
				b: b`)))
}

// writeLayerFiles writes layer files of a test to disk.
func (s *S) writeLayerFiles(c *C, layersDir string, inputs []*inputLayer) {
	err := os.MkdirAll(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, input := range inputs {
		err := ioutil.WriteFile(filepath.Join(layersDir, fmt.Sprintf("%03d-%s.yaml", input.order, input.label)), reindent(input.yaml), 0644)
		c.Assert(err, IsNil)
	}
}

const xField string = "x-field"

// xExtension implements the SectionExtension interface.
type xExtension struct{}

func (x xExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	xs := &xSection{}
	err := data.Decode(xs)
	if err != nil {
		return nil, err
	}
	// Propagate the name.
	for name, entry := range xs.Entries {
		if entry != nil {
			xs.Entries[name].Name = name
		}
	}
	return xs, nil
}

func (x xExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	xs := &xSection{}
	for _, section := range sections {
		err := xs.Combine(section)
		if err != nil {
			return nil, err
		}
	}
	return xs, nil
}

func (x xExtension) ValidatePlan(p *plan.Plan) error {
	var xs *xSection
	xs = p.Sections[xField].(*xSection)
	if xs != nil {
		var ys *ySection
		ys = p.Sections[yField].(*ySection)

		// Test dependency: Make sure every Y field in X refer to an existing Y entry.
		for xEntryField, xEntryValue := range xs.Entries {
			for _, yReference := range xEntryValue.Y {
				found := false
				for yEntryField, _ := range ys.Entries {
					if yReference == yEntryField {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("cannot find ySection entry %v as required by xSection entry %v ", yReference, xEntryField)
				}
			}
		}
	}
	return nil
}

// xSection is the backing type for xExtension.
type xSection struct {
	Entries map[string]*X `yaml:",inline,omitempty"`
}

func (xs *xSection) Validate() error {
	for field, entry := range xs.Entries {
		if entry == nil {
			return fmt.Errorf("cannot have nil entry for %q", field)
		}
		// Fictitious test requirement: entry names must start with x
		if !strings.HasPrefix(field, "x") {
			return fmt.Errorf("cannot accept entry not starting with letter 'x'")
		}
	}
	return nil
}

func (xs *xSection) IsZero() bool {
	return xs.Entries == nil
}

func (xs *xSection) Combine(other plan.Section) error {
	otherxSection, ok := other.(*xSection)
	if !ok {
		return fmt.Errorf("cannot combine incompatible section type")
	}

	for field, entry := range otherxSection.Entries {
		xs.Entries = makeMapIfNil(xs.Entries)
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := xs.Entries[field]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				xs.Entries[field] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			xs.Entries[field] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`invalid "override" value for entry %q`, field),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`unknown "override" value for entry %q`, field),
			}
		}
	}
	return nil
}

type X struct {
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`
	A        string        `yaml:"a,omitempty"`
	B        string        `yaml:"b,omitempty"`
	C        string        `yaml:"c,omitempty"`
	Y        []string      `yaml:"y-field,omitempty"`
}

func (x *X) Copy() *X {
	copied := *x
	copied.Y = append([]string(nil), x.Y...)
	return &copied
}

func (x *X) Merge(other *X) {
	if other.A != "" {
		x.A = other.A
	}
	if other.B != "" {
		x.B = other.B
	}
	if other.C != "" {
		x.C = other.C
	}
	x.Y = append(x.Y, other.Y...)
}

const yField string = "y-field"

// yExtension implements the SectionExtension interface.
type yExtension struct{}

func (y yExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	ys := &ySection{}
	err := data.Decode(ys)
	if err != nil {
		return nil, err
	}
	// Propagate the name.
	for name, entry := range ys.Entries {
		if entry != nil {
			ys.Entries[name].Name = name
		}
	}
	return ys, nil
}

func (y yExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	ys := &ySection{}
	for _, section := range sections {
		err := ys.Combine(section)
		if err != nil {
			return nil, err
		}
	}
	return ys, nil
}

func (y yExtension) ValidatePlan(p *plan.Plan) error {
	// This extension has no dependencies on the Plan to validate.
	return nil
}

// ySection is the backing type for yExtension.
type ySection struct {
	Entries map[string]*Y `yaml:",inline,omitempty"`
}

func (ys *ySection) Validate() error {
	for field, entry := range ys.Entries {
		if entry == nil {
			return fmt.Errorf("cannot have nil entry for %q", field)
		}
		// Fictitious test requirement: entry names must start with y
		if !strings.HasPrefix(field, "y") {
			return fmt.Errorf("cannot accept entry not starting with letter 'y'")
		}
	}
	return nil
}

func (ys *ySection) IsZero() bool {
	return ys.Entries == nil
}

func (ys *ySection) Combine(other plan.Section) error {
	otherySection, ok := other.(*ySection)
	if !ok {
		return fmt.Errorf("cannot combine incompatible section type")
	}

	for field, entry := range otherySection.Entries {
		ys.Entries = makeMapIfNil(ys.Entries)
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := ys.Entries[field]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				ys.Entries[field] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			ys.Entries[field] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`invalid "override" value for entry %q`, field),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`unknown "override" value for entry %q`, field),
			}
		}
	}
	return nil
}

type Y struct {
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`
	A        string        `yaml:"a,omitempty"`
	B        string        `yaml:"b,omitempty"`
	C        string        `yaml:"c,omitempty"`
}

func (y *Y) Copy() *Y {
	copied := *y
	return &copied
}

func (y *Y) Merge(other *Y) {
	if other.A != "" {
		y.A = other.A
	}
	if other.B != "" {
		y.B = other.B
	}
	if other.C != "" {
		y.C = other.C
	}
}

func makeMapIfNil[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		m = make(map[K]V)
	}
	return m
}
