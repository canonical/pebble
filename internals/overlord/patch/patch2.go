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

package patch

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/identities"
	"github.com/canonical/pebble/internals/overlord/state"
)

func init() {
	patches[2] = []PatchFunc{patch2}
}

// Load legacy identities if present (and new identities are not in Get/Set data).
func patch2(s *state.State) error {
	legacy := s.LegacyIdentities()
	if len(legacy) == 0 {
		return nil
	}
	if s.Has("identities") {
		logger.Noticef("WARNING: both new and legacy identities found in state file, ignoring legacy")
		return nil
	}

	var idents map[string]*identities.Identity
	err := json.Unmarshal(legacy, &idents)
	if err != nil {
		return fmt.Errorf("cannot unmarshal legacy identities: %v", err)
	}
	// A null entry would be stored as a nil identity and make the identities
	// manager panic on startup, so fail the migration instead. Check names in
	// sorted order so the error is deterministic.
	for _, name := range slices.Sorted(maps.Keys(idents)) {
		if idents[name] == nil {
			return fmt.Errorf("cannot migrate legacy identity %q: identity must not be nil", name)
		}
	}
	s.Set("identities", idents)
	logger.Noticef("Loaded legacy identities from state file")
	return nil
}
