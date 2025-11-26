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

import "maps"

// PatchesForTest returns the registered set of patches for testing purposes.
func PatchesForTest() map[int][]PatchFunc {
	return patches
}

// FakeLevel replaces the current implemented patch level
func FakeLevel(flevel, fsublevel int) (restore func()) {
	oldLevel := Level
	oldSublevel := Sublevel
	Level = flevel
	Sublevel = fsublevel
	oldPatches := make(map[int][]PatchFunc)
	maps.Copy(oldPatches, patches)

	for plevel, psublevels := range patches {
		if plevel > flevel {
			delete(patches, plevel)
			continue
		}
		if plevel == flevel && len(psublevels)-1 > fsublevel {
			psublevels = psublevels[:fsublevel+1]
			patches[plevel] = psublevels
		}
	}

	return func() {
		patches = oldPatches
		Level = oldLevel
		Sublevel = oldSublevel
	}
}
