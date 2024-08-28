// Copyright (C) 2024 Canonical Ltd
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type planSuite struct {
	planMgr   *planstate.PlanManager
	layersDir string

	writeLayerCounter int
}

var _ = Suite(&planSuite{})

func (ps *planSuite) SetUpTest(c *C) {
	ps.layersDir = filepath.Join(c.MkDir(), "layers")
	err := os.Mkdir(ps.layersDir, 0755)
	c.Assert(err, IsNil)

	//Reset write layer counter
	ps.writeLayerCounter = 1
}

func (ps *planSuite) writeLayer(c *C, layer string) {
	filename := fmt.Sprintf("%03[1]d-layer-file-%[1]d.yaml", ps.writeLayerCounter)
	err := os.WriteFile(filepath.Join(ps.layersDir, filename), []byte(layer), 0644)
	c.Assert(err, IsNil)

	ps.writeLayerCounter++
}

func (ps *planSuite) parseLayer(c *C, order int, label, layerYAML string) *plan.Layer {
	layer, err := plan.ParseLayer(order, label, []byte(layerYAML))
	c.Assert(err, IsNil)
	return layer
}

func (ps *planSuite) planLayersHasLen(c *C, expectedLen int) {
	plan := ps.planMgr.Plan()
	c.Assert(plan.Layers, HasLen, expectedLen)
}

func (ps *planSuite) planYAML(c *C) string {
	plan := ps.planMgr.Plan()
	yml, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	return string(yml)
}

// The YAML on tests passes through this function to deindent and
// replace tabs by spaces, so we can keep the code here sane.
func reindent(in string) []byte {
	var buf bytes.Buffer
	var trim string
	for _, line := range strings.Split(in, "\n") {
		if trim == "" {
			trimmed := strings.TrimLeft(line, "\t")
			if trimmed == "" {
				continue
			}
			if trimmed[0] == ' ' {
				panic("Tabs and spaces mixed early on string:\n" + in)
			}
			trim = line[:len(line)-len(trimmed)]
		}
		trimmed := strings.TrimPrefix(line, trim)
		if len(trimmed) == len(line) && strings.Trim(line, "\t ") != "" {
			panic("Line not indented consistently:\n" + line)
		}
		trimmed = strings.ReplaceAll(trimmed, "\t", "    ")
		buf.WriteString(trimmed)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

const testField string = "test-field"

// testExtension implements the LayerSectionExtension interface.
type testExtension struct{}

func (te testExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	ts := &testSection{}
	err := data.Decode(ts)
	if err != nil {
		return nil, err
	}
	// Populate Name.
	for name, entry := range ts.Entries {
		if entry != nil {
			ts.Entries[name].Name = name
		}
	}
	return ts, nil
}

func (te testExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	ts := &testSection{}
	for _, section := range sections {
		err := ts.Combine(section)
		if err != nil {
			return nil, err
		}
	}
	return ts, nil
}

func (te testExtension) ValidatePlan(p *plan.Plan) error {
	// This extension has no dependencies on the Plan to validate.
	return nil
}

// testSection is the backing type for testExtension.
type testSection struct {
	Entries map[string]*T `yaml:",inline"`
}

func (ts *testSection) Validate() error {
	// Fictitious test requirement: fields must start with t
	prefix := "t"
	for field, _ := range ts.Entries {
		if !strings.HasPrefix(field, prefix) {
			return fmt.Errorf("%q entry names must start with %q", testField, prefix)
		}
	}
	return nil
}

func (ts *testSection) IsZero() bool {
	return ts.Entries == nil
}

func (ts *testSection) Combine(other plan.Section) error {
	otherTSection, ok := other.(*testSection)
	if !ok {
		return fmt.Errorf("invalid section type")
	}

	for field, entry := range otherTSection.Entries {
		ts.Entries = makeMapIfNil(ts.Entries)
		switch entry.Override {
		case plan.MergeOverride:
			if old, ok := ts.Entries[field]; ok {
				copied := old.Copy()
				copied.Merge(entry)
				ts.Entries[field] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			ts.Entries[field] = entry.Copy()
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

type T struct {
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`
	A        string        `yaml:"a,omitempty"`
	B        string        `yaml:"b,omitempty"`
}

func (t *T) Copy() *T {
	copied := *t
	return &copied
}

func (t *T) Merge(other *T) {
	if other.A != "" {
		t.A = other.A
	}
	if other.B != "" {
		t.B = other.B
	}
}

func makeMapIfNil[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		m = make(map[K]V)
	}
	return m
}
