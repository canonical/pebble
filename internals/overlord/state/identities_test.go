// Copyright (c) 2024 Canonical Ltd
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

package state_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

type identitiesSuite struct{}

var _ = Suite(&identitiesSuite{})

// IMPORTANT NOTE: be sure secrets aren't included when adding to this!
func (s *identitiesSuite) TestMarshalAPI(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	err := st.AddIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	})
	c.Assert(err, IsNil)

	identities := st.Identities()
	data, err := json.MarshalIndent(identities, "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
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
    },
    "nancy": {
        "access": "metrics",
        "basic": {
            "password": "hash"
        }
    }
}`[1:])
}

func (s *identitiesSuite) TestUnmarshalAPI(c *C) {
	data := []byte(`
{
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
    },
    "nancy": {
        "access": "metrics",
        "basic": {
            "password": "hash"
        }
    }
}`)
	var identities map[string]*state.Identity
	err := json.Unmarshal(data, &identities)
	c.Assert(err, IsNil)
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	})
}

func (s *identitiesSuite) TestUnmarshalAPIErrors(c *C) {
	tests := []struct {
		data  string
		error string
	}{{
		data:  `{"no-type": {"access": "admin"}}`,
		error: `identity must have at least one type \("local" or "basic"\)`,
	}, {
		data:  `{"invalid-access": {"access": "admin", "local": {}}}`,
		error: `local identity must specify user-id`,
	}, {
		data:  `{"invalid-access": {"access": "metrics", "basic": {}}}`,
		error: `basic identity must specify password`,
	}, {
		data:  `{"invalid-access": {"access": "foo", "local": {"user-id": 42}}}`,
		error: `invalid access value "foo", must be "admin", "read", "metrics", or "untrusted"`,
	}, {
		data:  `{"invalid-access": {"local": {"user-id": 42}}}`,
		error: `access value must be specified \("admin", "read", "metrics", or "untrusted"\)`,
	}}
	for _, test := range tests {
		c.Logf("Input data: %s", test.data)
		var identities map[string]*state.Identity
		err := json.Unmarshal([]byte(test.data), &identities)
		c.Check(err, ErrorMatches, test.error)
	}
}

func (s *identitiesSuite) TestMarshalState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	err := st.AddIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)

	// Marshal entire state, then pull out just the "identities" key to test that.
	data, err := json.Marshal(st)
	c.Assert(err, IsNil)
	var unmarshalled map[string]any
	err = json.Unmarshal(data, &unmarshalled)
	c.Assert(err, IsNil)
	data, err = json.MarshalIndent(unmarshalled["identities"], "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
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
}`[1:])
}

func (s *identitiesSuite) TestUnmarshalState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	data := []byte(`
{
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
	err := json.Unmarshal(data, &st)
	c.Assert(err, IsNil)
	c.Assert(st.Identities(), DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
}

func (s *identitiesSuite) TestAddIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	// Ensure they were added correctly (and Name fields have been set).
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	})

	// Can't add identity names that already exist.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, "identities already exist: bob, mary")

	// Can't add a nil identity.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": nil,
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: identity must not be nil`)

	// Access value must be valid.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: "bar",
			Local:  &state.LocalIdentity{UserID: 43},
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: invalid access value "bar", must be "admin", "read", "metrics", or "untrusted"`)

	// Must have at least one type.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: "admin",
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: identity must have at least one type \(\"local\" or \"basic\"\)`)

	// Ensure user IDs are unique with existing users.
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 1000 \(bill, mary\)`)

	// Ensure user IDs are unique among the ones being added (and test >2 with same UID).
	err = st.AddIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
		"bale": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
		"boll": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2000},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 2000 \(bale, bill, boll\)`)
}

func (s *identitiesSuite) TestUpdateIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"mary": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "new hash"},
		},
	})
	c.Assert(err, IsNil)

	// Ensure they were updated correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"mary": {
			Name:   "mary",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "new hash"},
		},
	})

	// Can't update identity names that don't exist.
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bale": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, ErrorMatches, "identities do not exist: bale, bill")

	// Ensure validation is being done (full testing done in AddIdentity).
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": nil,
	})
	c.Assert(err, ErrorMatches, `identity "bob" invalid: identity must not be nil`)

	// Ensure unique user ID testing is being done (full testing done in AddIdentity).
	err = st.UpdateIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 42 \(bob, mary\)`)
}

func (s *identitiesSuite) TestReplaceIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bob": nil, // nil means remove it
		"mary": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"newguy": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 44},
		},
	})
	c.Assert(err, IsNil)

	// Ensure they were added/updated/deleted correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"mary": {
			Name:   "mary",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"newguy": {
			Name:   "newguy",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 44},
		},
	})

	// Ensure validation is being done (full testing done in AddIdentity).
	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bill": {
			Access: "admin",
		},
	})
	c.Assert(err, ErrorMatches, `identity "bill" invalid: identity must have at least one type \("local" or "basic"\)`)

	// Ensure unique user ID testing is being done (full testing done in AddIdentity).
	err = st.ReplaceIdentities(map[string]*state.Identity{
		"bob": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
	})
	c.Assert(err, ErrorMatches, `cannot have multiple identities with user ID 43 \(bob, mary\)`)
}

func (s *identitiesSuite) TestRemoveIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bill": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
		"queen": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1001},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	err = st.RemoveIdentities(map[string]struct{}{
		"bob":   {},
		"mary":  {},
		"nancy": {},
	})
	c.Assert(err, IsNil)

	// Ensure they were removed correctly.
	identities := st.Identities()
	c.Assert(identities, DeepEquals, map[string]*state.Identity{
		"bill": {
			Name:   "bill",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 43},
		},
		"queen": {
			Name:   "queen",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1001},
		},
	})

	// Can't remove identity names that don't exist.
	err = st.RemoveIdentities(map[string]struct{}{
		"bill": {},
		"bale": {},
		"mary": {},
	})
	c.Assert(err, ErrorMatches, "identities do not exist: bale, mary")
}

func (s *identitiesSuite) TestIdentities(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	// Ensure it returns correct results.
	identities := st.Identities()
	expected := map[string]*state.Identity{
		"bob": {
			Name:   "bob",
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Name:   "mary",
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Name:   "nancy",
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: "hash"},
		},
	}
	c.Assert(identities, DeepEquals, expected)

	// Ensure the map was cloned (mutations to first map won't affect second).
	identities2 := st.Identities()
	c.Assert(identities2, DeepEquals, expected)
	identities["changed"] = &state.Identity{}
	c.Assert(identities2, DeepEquals, expected)
}

func (s *identitiesSuite) TestIdentityFromInputs(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	original := map[string]*state.Identity{
		"bob": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"nancy": {
			Access: state.MetricsAccess,
			Basic: &state.BasicIdentity{
				// password: test
				Password: "$6$F9cFSVEKyO4gB1Wh$8S1BSKsNkF.jBAixGc4W7l80OpfCNk65LZBDHBng3NAmbcHuMj4RIm7992rrJ8YA.SJ0hvm.vGk2z483am4Ym1",
			},
		},
	}
	err := st.AddIdentities(original)
	c.Assert(err, IsNil)

	identity := st.IdentityFromInputs(nil, "", "")
	c.Assert(identity, IsNil)

	userID := uint32(0)
	identity = st.IdentityFromInputs(&userID, "", "")
	c.Assert(identity, IsNil)

	userID = 100
	identity = st.IdentityFromInputs(&userID, "", "")
	c.Assert(identity, IsNil)

	userID = 42
	identity = st.IdentityFromInputs(&userID, "", "")
	c.Assert(identity, NotNil)
	c.Check(identity.Name, Equals, "bob")

	userID = 1000
	identity = st.IdentityFromInputs(&userID, "", "")
	c.Assert(identity, NotNil)
	c.Check(identity.Name, Equals, "mary")

	identity = st.IdentityFromInputs(nil, "nancy", "test")
	c.Assert(identity, NotNil)
	c.Check(identity.Name, Equals, "nancy")
}
