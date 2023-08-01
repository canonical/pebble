// Copyright (c) 2014-2020 Canonical Ltd
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
	"github.com/canonical/pebble/internals/overlord/state"
)

func init() {
	patches[1] = []PatchFunc{patch1} // Append here patch1_1, patch1_2, etc.
}

// patch1 is an empty patch and serves as an example for the real ones.
// Do NOT replace the logic below by a real patch, as this patch will have
// been applied in systems for real already and have no effect.
func patch1(s *state.State) error {

	// Here you can have any logic you want manipulating s and the
	// system itself when required to reflect such changes.

	// While working on the patch keep in mind that it may run partially
	// or fully again when a failure occurs, and this will happen until it
	// works completely.

	// Also remember that patching the patch after it goes public only has
	// effect on whoever hasn't applied it completely yet. So this may be
	// done to fix the application of the patch in failure scenarios, but
	// it cannot be used to improve the patch in an incompatible way.

	// Be *very* careful when importing code from elsewhere to help
	// with state setting here. The actual code elsehwere will always
	// reflect the most recent patch level at tip, but the patch here
	// needs to move between patch versions OLD and OLD+1, and both
	// of those will get old and out of sync with tip.

	// You don't need to worry about persisting changes as that's handled
	// by the machinery around this patch.

	var something string
	err := s.Get("something-in-test", &something)
	if err == nil && something == "old" {
		s.Set("something-in-test", "new")
	}

	return nil
}
