// Copyright (c) 2026 Canonical Ltd
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
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/identities"
	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/state"
)

type patch2Suite struct{}

var _ = Suite(&patch2Suite{})

func (s *patch2Suite) TestLegacyIdentities(c *C) {
	restore := patch.FakeLevel(2, 1)
	defer restore()

	data := []byte(`
{
    "data": {"patch-level": 1},
    "identities": {
        "bob": {
            "access": "read",
            "local": {
                "user-id": 42
            }
        },
        "mary": {
            "access": "admin",
            "local": {
                "user-id": 1000
            }
        }
    }
}`)

	st, err := state.ReadState(nil, bytes.NewReader(data))
	c.Assert(err, IsNil)
	err = patch.Apply(st)
	c.Assert(err, IsNil)
	mgr, err := identities.NewManager(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(mgr.Identities(), DeepEquals, map[string]*identities.Identity{
		"bob": {
			Name:   "bob",
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})

	// ensure we moved forward to patch-level 2 (sublevel 0)
	var patchLevel int
	err = st.Get("patch-level", &patchLevel)
	c.Assert(err, IsNil)
	c.Assert(patchLevel, Equals, 2)
	err = st.Get("patch-sublevel", &patchLevel)
	c.Assert(err, IsNil)
	c.Assert(patchLevel, Equals, 0)
}

func (s *patch2Suite) TestNewAndLegacyIdentities(c *C) {
	restore := patch.FakeLevel(2, 1)
	defer restore()

	// If both new and legacy are present, it should prefer the new
	// (and emit a warning log, but we don't test for that).
	data := []byte(`
{
	"data": {
		"patch-level": 1,
		"identities": {
			"bob": {
				"access": "read",
				"local": {
					"user-id": 42
				}
			}
		}
	},
    "identities": {
        "mary": {
            "access": "admin",
            "local": {
                "user-id": 1000
            }
        }
    }
}`)

	st, err := state.ReadState(nil, bytes.NewReader(data))
	c.Assert(err, IsNil)
	err = patch.Apply(st)
	c.Assert(err, IsNil)
	mgr, err := identities.NewManager(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(mgr.Identities(), DeepEquals, map[string]*identities.Identity{
		"bob": {
			Name:   "bob",
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
	})
}

func (s *patch2Suite) TestNullLegacyIdentities(c *C) {
	restore := patch.FakeLevel(2, 1)
	defer restore()

	tests := []struct {
		summary    string
		identities string
		error      string
	}{{
		summary:    "single null identity",
		identities: `{"bob": null}`,
		error:      `.*cannot migrate legacy identity "bob": identity must not be nil`,
	}, {
		summary:    "null identity alongside a valid one",
		identities: `{"bob": {"access": "read", "local": {"user-id": 42}}, "mary": null}`,
		error:      `.*cannot migrate legacy identity "mary": identity must not be nil`,
	}, {
		summary:    "multiple null identities report the first by name",
		identities: `{"mary": null, "bob": null}`,
		error:      `.*cannot migrate legacy identity "bob": identity must not be nil`,
	}}

	for _, test := range tests {
		c.Logf("Test: %s", test.summary)
		data := []byte(`{"data": {"patch-level": 1}, "identities": ` + test.identities + `}`)
		st, err := state.ReadState(nil, bytes.NewReader(data))
		c.Assert(err, IsNil)

		err = patch.Apply(st)
		c.Assert(err, ErrorMatches, test.error)

		// The migration must not have been applied.
		st.Lock()
		c.Check(st.Has("identities"), Equals, false)
		var patchLevel int
		err = st.Get("patch-level", &patchLevel)
		st.Unlock()
		c.Assert(err, IsNil)
		c.Check(patchLevel, Equals, 1)
	}
}
