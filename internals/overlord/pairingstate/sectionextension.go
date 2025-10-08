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

package pairingstate

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
)

var _ plan.SectionExtension = (*SectionExtension)(nil)

// PairingField is the top level string key used in the Pebble plan.
const PairingField string = "pairing"

// SectionExtension implements the Pebble plan.SectionExtension interface.
type SectionExtension struct{}

func (s SectionExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	p := &PairingConfig{}
	// The following issue prevents us from using the yaml.Node decoder
	// with KnownFields = true behaviour. Once one of the proposals gets
	// merged, we can remove the intermediate Marshal step.
	// https://github.com/go-yaml/yaml/issues/460
	if len(data.Content) != 0 {
		yml, err := yaml.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot marshal pairing section: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(yml))
		dec.KnownFields(true)
		if err = dec.Decode(p); err != nil {
			return nil, &plan.FormatError{
				Message: fmt.Sprintf("cannot parse the pairing section: %v", err),
			}
		}
	}
	return p, nil
}

func (s SectionExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	p := &PairingConfig{}
	for _, section := range sections {
		pairingLayer, ok := section.(*PairingConfig)
		if !ok {
			return nil, fmt.Errorf("internal error: invalid section type %T", section)
		}
		p.Combine(pairingLayer)
	}
	return p, nil
}

func (s SectionExtension) ValidatePlan(p *plan.Plan) error {
	// No dependencies to validate in the plan.
	return nil
}
