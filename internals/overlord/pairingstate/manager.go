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

// pairingstate manages client server pairing.
package pairingstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// Mode controls the pairing policy of the pairing manager.
type mode string

const (
	// Unset (no pairing is possible)
	modeUnset mode = ""
	// No pairing is possible
	modeDisabled mode = "disabled"
	// Only a single client can pair.
	modeSingle mode = "single"
	// Multiple clients can pair.
	modeMultiple mode = "multiple"
)

type PairingConfig struct {
	Mode mode `yaml:"mode"`
}

func (c *PairingConfig) Validate() error {
	switch c.Mode {
	case modeUnset, modeDisabled, modeSingle, modeMultiple:
	default:
		return fmt.Errorf("cannot support pairing mode %q: unknown mode", c.Mode)
	}
	return nil
}

func (c *PairingConfig) IsZero() bool {
	return c.Mode == modeUnset
}

func (c *PairingConfig) combine(other *PairingConfig) {
	if other.Mode != modeUnset {
		c.Mode = other.Mode
	}
}

type PairingManager struct {
	state  *state.State
	mu     sync.Mutex
	config *PairingConfig
}

func NewManager(state *state.State) *PairingManager {
	m := &PairingManager{
		state: state,
		config: &PairingConfig{
			Mode: modeDisabled,
		},
	}
	return m
}

// PlanChanged informs the pairing manager that the plan has been updated.
func (m *PairingManager) PlanChanged(update *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = update.Sections[PairingField].(*PairingConfig)
}

// Ensure implements StateManager.Ensure.
func (m *PairingManager) Ensure() error {
	return nil
}
