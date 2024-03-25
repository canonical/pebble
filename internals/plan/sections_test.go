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
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
)

// planInput can be a file input, or an input via the
// API for either a layer append or layer update
type planInput struct {
	order int
	label string
	yaml  string
}

// planResult represents the combined plan
type planResult struct {
	order   int
	label   string
	summary string
	desc    string
	x       *XSection
	y       *YSection
}

type sectionExt struct {
	key string
	ext plan.LayerSectionExtension
}

var planExtTests = []struct {
	planSectionExtensions []sectionExt
	files                 []*planInput

	result      *planResult
	resultYaml  string
	errorString string
}{
	// Index 0: No Sections
	{
		resultYaml: string(reindent(`
			{}`)),
	},
	// Index 1: Add empty sections
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		resultYaml: string(reindent(`
			{}`)),
	},
	// Index 2: Load file layers invalid section
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
					invalid:
				`,
			},
		},
		errorString: "cannot parse layer .*: unknown section .*",
	},
	// Index 3: Load file layers not unique order
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-1",
				yaml: `
					summary: xy
					description: desc
				`,
			},
			&planInput{
				order: 1,
				label: "layer-2",
				yaml: `
					summary: xy
					description: desc
				`,
			},
		},
		errorString: "invalid layer filename: .* not unique .*",
	},
	// Index 4: Load file layers not unique label
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
				`,
			},
			&planInput{
				order: 2,
				label: "layer-xy",
				yaml: `
					summary: xy
					description: desc
				`,
			},
		},
		errorString: "invalid layer filename: .* not unique .*",
	},
	// Index 5: Load file layers with section validation failure
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x-key:
						z1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		errorString: "XSection keys must start with x",
	},
	// Index 6: Load file layers failed plan validation
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x-key:
						x1:
							override: replace
							a: a
							b: b
							y-key:
							  - y2
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y-key:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		errorString: "cannot find .* as required by .*",
	},
	// Index 7: Load file layers
	{
		planSectionExtensions: []sectionExt{
			sectionExt{
				key: "x-key",
				ext: &XExt{},
			},
			sectionExt{
				key: "y-key",
				ext: &YExt{},
			},
		},
		files: []*planInput{
			&planInput{
				order: 1,
				label: "layer-x",
				yaml: `
					summary: x
					description: desc-x
					x-key:
						x1:
							override: replace
							a: a
							b: b
							y-key:
							  - y1
				`,
			},
			&planInput{
				order: 2,
				label: "layer-y",
				yaml: `
					summary: y
					description: desc-y
					y-key:
						y1:
							override: replace
							a: a
							b: b
				`,
			},
		},
		result: &planResult{
			summary: "y",
			desc:    "desc-y",
			x: &XSection{
				Entries: map[string]*X{
					"x1": &X{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
						Y: []string{
							"y1",
						},
					},
				},
			},
			y: &YSection{
				Entries: map[string]*Y{
					"y1": &Y{
						Override: plan.ReplaceOverride,
						A:        "a",
						B:        "b",
					},
				},
			},
		},
		resultYaml: string(reindent(`
			x-key:
				x1:
					override: replace
					a: a
					b: b
					y-key:
						- y1
			y-key:
				y1:
					override: replace
					a: a
					b: b`)),
	},
}

func (s *S) TestPlanSections(c *C) {
	for testIndex, planTest := range planExtTests {
		c.Logf("Running TestPlan() with test data index %v", testIndex)

		baseDir := c.MkDir()

		// Write all the YAML data to disk in a temporary location
		s.writeLayerFiles(c, baseDir, planTest.files)

		p := plan.NewPlan()

		fail := func(c *C) error {
			var err error

			// Add types
			for _, sectionExt := range planTest.planSectionExtensions {
				p.AddSectionExtension(sectionExt.key, sectionExt.ext)
			}

			// Load the plan layers
			err = p.ReadDir(baseDir)
			if err != nil {
				return err
			}

			return nil
		}(c)

		if fail != nil {
			c.Assert(fail, ErrorMatches, planTest.errorString)
		} else {

			if planTest.result != nil {
				// Check the plan against the test result
				c.Assert(p.Combined.Summary(), Equals, planTest.result.summary)
				c.Assert(p.Combined.Description(), Equals, planTest.result.desc)

				// XSection
				x := p.Combined.Section(XKey).(*XSection)
				c.Assert(x.Entries, DeepEquals, planTest.result.x.Entries)

				// YSection
				y := p.Combined.Section(YKey).(*YSection)
				c.Assert(y.Entries, DeepEquals, planTest.result.y.Entries)
			}

			// YAML validate
			planYAML, err := yaml.Marshal(p.Combined)
			c.Assert(err, IsNil)
			c.Assert(string(planYAML), Equals, planTest.resultYaml)
		}
	}
}

// Layer Section X source file

// Validation of section X depend on access to section Y.

const XKey string = "x-key"

// Layer Section X Extension
type XExt struct{}

func (x XExt) NewSection() plan.LayerSection {
	return NewXSection()
}

func (x XExt) ParseSection(data *yaml.Node) (plan.LayerSection, error) {
	xs := x.NewSection().(*XSection)
	err := data.Decode(xs)
	if err != nil {
		return nil, err
	}

	return xs, nil
}

func (x XExt) CombineSections(sections ...plan.LayerSection) (plan.LayerSection, error) {
	xs := NewXSection()
	for _, section := range sections {
		err := xs.Combine(section)
		if err != nil {
			return nil, err
		}
	}
	return xs, nil
}

func (x XExt) ValidatePlan(combinedPlan *plan.CombinedPlan) error {
	// Let's validate YSection keys exist
	ys := combinedPlan.Section(YKey).(*YSection)

	// Make sure every Y key in X refer to an existing Y entry.
	xs := combinedPlan.Section(XKey).(*XSection)
	for keyX, entryX := range xs.Entries {
		for _, refY := range entryX.Y {
			found := false
			for keyY, _ := range ys.Entries {
				if refY == keyY {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("cannot find YSection entry %v as required by XSection entry %v ", refY, keyX)
			}
		}
	}
	return nil
}

// Layer Section X
type XSection struct {
	Entries map[string]*X `yaml:",inline"`
}

func NewXSection() *XSection {
	xs := &XSection{
		Entries: make(map[string]*X),
	}
	return xs
}

func (xs *XSection) IsEmpty() bool {
	return len(xs.Entries) == 0
}

func (xs *XSection) Validate() error {
	// Test requirement: keys must start with x
	for key, _ := range xs.Entries {
		if !strings.HasPrefix(key, "x") {
			return fmt.Errorf("XSection keys must start with x")
		}
	}
	return nil
}

func (xs *XSection) Combine(other plan.LayerSection) error {
	otherXSection, ok := other.(*XSection)
	if !ok {
		return fmt.Errorf("invalid section type")
	}

	for key, entry := range otherXSection.Entries {
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := xs.Entries[key]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				xs.Entries[key] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			xs.Entries[key] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`invalid "override" value for entry %q`, key),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`unknown "override" value for entry %q`, key),
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
	Y        []string      `yaml:"y-key,omitempty"`
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
	x.Y = append(x.Y, other.Y...)
}

// Layer Section Y source file

const YKey string = "y-key"

// Layer Section Y Extension
type YExt struct{}

func (y YExt) NewSection() plan.LayerSection {
	return NewYSection()
}

func (y YExt) ParseSection(data *yaml.Node) (plan.LayerSection, error) {
	ys := y.NewSection().(*YSection)
	err := data.Decode(ys)
	if err != nil {
		return nil, err
	}
	return ys, nil
}

func (y YExt) CombineSections(sections ...plan.LayerSection) (plan.LayerSection, error) {
	ys := NewYSection()
	for _, section := range sections {
		err := ys.Combine(section)
		if err != nil {
			return nil, err
		}
	}
	return ys, nil
}

func (y YExt) ValidatePlan(combinedPlan *plan.CombinedPlan) error {
	return nil
}

// Layer Section Y
type YSection struct {
	Entries map[string]*Y `yaml:",inline"`
}

func NewYSection() *YSection {
	ys := &YSection{
		Entries: make(map[string]*Y),
	}
	return ys
}

func (ys *YSection) IsEmpty() bool {
	return len(ys.Entries) == 0
}

func (ys *YSection) Validate() error {
	// Test requirement: keys must start with y
	for key, _ := range ys.Entries {
		if !strings.HasPrefix(key, "y") {
			return fmt.Errorf("YSection keys must start with y")
		}
	}
	return nil
}

func (ys *YSection) Combine(other plan.LayerSection) error {
	otherYSection, ok := other.(*YSection)
	if !ok {
		return fmt.Errorf("invalid section type")
	}

	for key, entry := range otherYSection.Entries {
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := ys.Entries[key]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				ys.Entries[key] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			ys.Entries[key] = entry.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`invalid "override" value for entry %q`, key),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`unknown "override" value for entry %q`, key),
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
}

func (s *S) writeLayerFiles(c *C, baseDir string, inputs []*planInput) {
	layersDir := filepath.Join(baseDir, "layers")
	err := os.MkdirAll(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, input := range inputs {
		err := ioutil.WriteFile(filepath.Join(layersDir, fmt.Sprintf("%03d-%s.yaml", input.order, input.label)), reindent(input.yaml), 0644)
		c.Assert(err, IsNil)
	}
}
