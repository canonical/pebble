// Copyright (c) 2025 Canonical Ltd
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

package pairingstate_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
)

var schemaTests = []struct {
	summary         string
	layers          []string
	combinedSection *pairingstate.PairingConfig
	combinedYAML    string
	error           string
}{{
	summary:         "empty section",
	combinedSection: &pairingstate.PairingConfig{},
	combinedYAML:    `pairing: {}`,
}, {
	summary: "disabled mode",
	layers: []string{`
pairing:
    mode: disabled
    `},
	combinedSection: &pairingstate.PairingConfig{
		Mode: pairingstate.ModeDisabled,
	},
	combinedYAML: `
pairing:
    mode: disabled
    `,
}, {
	summary: "single mode",
	layers: []string{`
pairing:
    mode: single
    `},
	combinedSection: &pairingstate.PairingConfig{
		Mode: pairingstate.ModeSingle,
	},
	combinedYAML: `
pairing:
    mode: single
    `,
}, {
	summary: "multiple mode",
	layers: []string{`
pairing:
    mode: multiple
    `},
	combinedSection: &pairingstate.PairingConfig{
		Mode: pairingstate.ModeMultiple,
	},
	combinedYAML: `
pairing:
    mode: multiple
    `,
}, {
	summary: "invalid mode",
	layers: []string{`
pairing:
    mode: invalid
    `},
	error: `invalid pairing mode \"invalid\": should be \"disabled\", \"single\" or \"multiple\"`,
}, {
	summary: "unknown field",
	layers: []string{`
pairing:
    mode: single
    unknown-field: value
    `},
	error: `cannot parse the pairing section: yaml: unmarshal errors:\n  line 2: field unknown-field not found in type pairingstate.pairingConfig`,
}, {
	summary: "mode merge works",
	layers: []string{`
pairing:
    mode: disabled
    `, `
pairing:
    mode: single
    `},
	combinedSection: &pairingstate.PairingConfig{
		Mode: pairingstate.ModeSingle,
	},
	combinedYAML: `
pairing:
    mode: single
    `,
}, {
	summary: "empty mode in later layer doesn't override",
	layers: []string{`
pairing:
    mode: single
    `, `
pairing:
    `},
	combinedSection: &pairingstate.PairingConfig{
		Mode: pairingstate.ModeSingle,
	},
	combinedYAML: `
pairing:
    mode: single
    `,
}}

func (s *pairingSuite) TestPairingSectionExtensionSchema(c *C) {

	for i, t := range schemaTests {
		c.Logf("Running TestPairingSectionExtensionSchema %q test using test data index %d\n", t.summary, i)
		combined, err := parseCombineLayers(t.layers)
		if t.error != "" {
			c.Assert(err, ErrorMatches, t.error)
		} else {
			c.Assert(err, IsNil)
			section, ok := combined.Sections[pairingstate.PairingField]
			c.Assert(ok, Equals, true)
			c.Assert(section, NotNil)
			ps, ok := section.(*pairingstate.PairingConfig)
			c.Assert(ok, Equals, true)
			c.Assert(ps, DeepEquals, t.combinedSection)
			c.Assert(layerYAML(c, combined), Equals, strings.TrimSpace(t.combinedYAML))
		}
	}
}
