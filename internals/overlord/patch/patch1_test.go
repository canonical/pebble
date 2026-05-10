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

package patch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/state"
)

// Analogous to patch1.go itself, this is available as inspiration for actual patch tests.

type patch1Suite struct {
	statePath string
}

func TestPatch1Suite(t *testing.T) {
	tc.Run(t, &patch1Suite{})
}

var stateBeforePatch1 = []byte(`
{
	"data": {
                "patch-level": 0,
		"something-in-test": "old"
	}
}
`)

func (s *patch1Suite) SetUpTest(c *tc.C) {
	s.statePath = filepath.Join(c.MkDir(), "state.json")
	err := os.WriteFile(s.statePath, stateBeforePatch1, 0644)
	c.Assert(err, tc.ErrorIsNil)

	c.Cleanup(func() {
		s.statePath = ""
	})
}

func (s *patch1Suite) TestPatch1(c *tc.C) {
	r, err := os.Open(s.statePath)
	c.Assert(err, tc.ErrorIsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, tc.ErrorIsNil)

	// go from patch-level 0 to patch-level 1
	restorer := patch.FakeLevel(1, 1)
	defer restorer()

	err = patch.Apply(st)
	c.Assert(err, tc.ErrorIsNil)

	st.Lock()
	defer st.Unlock()

	// ensure we only moved forward to patch-level 1
	var patchLevel int
	err = st.Get("patch-level", &patchLevel)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(patchLevel, tc.Equals, 1)

	err = st.Get("patch-sublevel", &patchLevel)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(patchLevel, tc.Equals, 0)

	var something string
	err = st.Get("something-in-test", &something)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(something, tc.Equals, "new")
}
