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
	ps.layersDir = c.MkDir()

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
