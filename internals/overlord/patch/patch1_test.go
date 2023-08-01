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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/state"
)

// Analogous to patch1.go itself, this is available as inspiration for actual patch tests.

type patch1Suite struct {
	statePath string
}

var _ = Suite(&patch1Suite{})

var stateBeforePatch1 = []byte(`
{
	"data": {
                "patch-level": 0,
		"something-in-test": "old"
	}
}
`)

func (s *patch1Suite) SetUpTest(c *C) {
	s.statePath = filepath.Join(c.MkDir(), "state.json")
	err := ioutil.WriteFile(s.statePath, stateBeforePatch1, 0644)
	c.Assert(err, IsNil)
}

func (s *patch1Suite) TestPatch1(c *C) {
	r, err := os.Open(s.statePath)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	// go from patch-level 0 to patch-level 1
	restorer := patch.FakeLevel(1, 1)
	defer restorer()

	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	// ensure we only moved forward to patch-level 1
	var patchLevel int
	err = st.Get("patch-level", &patchLevel)
	c.Assert(err, IsNil)
	c.Assert(patchLevel, Equals, 1)

	err = st.Get("patch-sublevel", &patchLevel)
	c.Assert(err, IsNil)
	c.Assert(patchLevel, Equals, 0)

	var something string
	err = st.Get("something-in-test", &something)
	c.Assert(err, IsNil)
	c.Assert(something, Equals, "new")
}
