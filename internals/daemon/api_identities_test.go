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

package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/identities"
)

func (s *apiSuite) TestIdentities(c *C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	identitiesMgr := s.d.overlord.IdentitiesManager()
	err := identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"bob": {
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/identities", nil)
	c.Assert(err, IsNil)
	cmd := apiCmd("/v1/identities")
	rsp, ok := cmd.GET(cmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	identities, ok := rsp.Result.(map[string]*apiIdentity)
	c.Assert(ok, Equals, true)

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
    }
}`[1:])
}

func (s *apiSuite) TestAddIdentities(c *C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	s.daemon(c)

	body := `
{
    "action": "add",
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
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)

	st := s.d.overlord.State()
	st.Lock()
	identitiesMgr := s.d.overlord.IdentitiesManager()
	idents := identitiesMgr.Identities()
	c.Assert(idents, DeepEquals, map[string]*identities.Identity{
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
	st.Unlock()

	ensureSecurityLog(c, logBuf.String(), "WARN", "user_created:<unknown>,bob,read", "Creating read user bob")
	ensureSecurityLog(c, logBuf.String(), "WARN", "user_created:<unknown>,mary,admin", "Creating admin user mary")
}

func (s *apiSuite) TestAddIdentitiesNull(c *C) {
	s.daemon(c)

	body := `
{
    "action": "add",
    "identities": {
        "mary": null
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, `identity value for "mary" must not be null for add operation`)
}

func (s *apiSuite) TestUpdateIdentities(c *C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	identitiesMgr := s.d.overlord.IdentitiesManager()
	err := identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"bob": {
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	body := `
{
    "action": "update",
    "identities": {
        "bob": {
            "access": "admin",
            "local": {
                "user-id": 1000
            }
        },
        "mary": {
            "access": "read",
            "local": {
                "user-id": 42
            }
        }
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)

	st.Lock()
	idents := identitiesMgr.Identities()
	c.Assert(idents, DeepEquals, map[string]*identities.Identity{
		"bob": {
			Name:   "bob",
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
		"mary": {
			Name:   "mary",
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
	})
	st.Unlock()

	ensureSecurityLog(c, logBuf.String(), "WARN", "user_updated:<unknown>,bob,admin", "Updating admin user bob")
	ensureSecurityLog(c, logBuf.String(), "WARN", "user_updated:<unknown>,mary,read", "Updating read user mary")
}

func (s *apiSuite) TestUpdateIdentitiesNull(c *C) {
	s.daemon(c)

	body := `
{
    "action": "update",
    "identities": {
        "mary": null
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, `identity value for "mary" must not be null for update operation`)
}

func (s *apiSuite) TestReplaceIdentities(c *C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	identitiesMgr := s.d.overlord.IdentitiesManager()
	err := identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"bob": {
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	body := `
{
    "action": "replace",
    "identities": {
        "bob": null,
        "mary": {
            "access": "read",
            "local": {
                "user-id": 43
            }
        },
        "newguy": {
            "access": "admin",
            "local": {
                "user-id": 44
            }
        }
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)

	st.Lock()
	idents := identitiesMgr.Identities()
	c.Assert(idents, DeepEquals, map[string]*identities.Identity{
		"mary": {
			Name:   "mary",
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 43},
		},
		"newguy": {
			Name:   "newguy",
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 44},
		},
	})
	st.Unlock()

	ensureSecurityLog(c, logBuf.String(), "WARN", "user_deleted:<unknown>,bob", "Deleting user bob")
	ensureSecurityLog(c, logBuf.String(), "WARN", "user_updated:<unknown>,mary,read", "Updating read user mary")
	ensureSecurityLog(c, logBuf.String(), "WARN", "user_updated:<unknown>,newguy,admin", "Updating admin user newguy")
}

func (s *apiSuite) TestRemoveIdentities(c *C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	identitiesMgr := s.d.overlord.IdentitiesManager()
	err := identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"bob": {
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 42},
		},
		"mary": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	body := `
{
    "action": "remove",
    "identities": {
        "bob": null
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)

	st.Lock()
	idents := identitiesMgr.Identities()
	c.Assert(idents, DeepEquals, map[string]*identities.Identity{
		"mary": {
			Name:   "mary",
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
	})
	st.Unlock()

	ensureSecurityLog(c, logBuf.String(), "WARN", "user_deleted:<unknown>,bob", "Deleting user bob")
}

func (s *apiSuite) TestRemoveIdentitiesNotNull(c *C) {
	s.daemon(c)

	body := `
{
    "action": "remove",
    "identities": {
        "mary": {
            "access": "read",
            "local": {
                "user-id": 43
            }
        }
    }
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, `identity value for "mary" must be null for remove operation`)
}

func (s *apiSuite) TestPostIdentitiesInvalidAction(c *C) {
	s.daemon(c)

	body := `
{
    "action": "foobar",
    "identities": {}
}`
	rsp := s.postIdentities(c, body)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, `invalid action "foobar", must be "add", "update", "replace", or "remove"`)
}

func (s *apiSuite) TestUnmarshalErrors(c *C) {
	s.daemon(c)

	tests := []struct {
		data  string
		error string
	}{{
		data:  `{"no-type": {"access": "admin"}}`,
		error: `identity must have at least one type \("local", "basic", or "cert"\)`,
	}, {
		data:  `{"invalid-access": {"access": "admin", "local": {}}}`,
		error: `local identity must specify user-id`,
	}, {
		data:  `{"invalid-access": {"access": "metrics", "basic": {}}}`,
		error: `basic identity must specify password \(hashed\)`,
	}, {
		data:  `{"invalid-access": {"access": "foo", "local": {"user-id": 42}}}`,
		error: `invalid access value "foo", must be "admin", "read", "metrics", or "untrusted"`,
	}, {
		data:  `{"invalid-access": {"local": {"user-id": 42}}}`,
		error: `access value must be specified \("admin", "read", "metrics", or "untrusted"\)`,
	}}
	for _, test := range tests {
		c.Logf("Input data: %s", test.data)

		body := fmt.Sprintf(`{"action": "foobar", "identities": %s}`, test.data)

		rsp := s.postIdentities(c, body)
		c.Check(rsp.Type, Equals, ResponseTypeError)
		c.Check(rsp.Status, Equals, http.StatusBadRequest)
		result, ok := rsp.Result.(*errorResult)
		c.Assert(ok, Equals, true)
		c.Check(result.Message, Matches, ".*: "+test.error)
	}
}

func (s *apiSuite) postIdentities(c *C, body string) *resp {
	req, err := http.NewRequest("POST", "/v1/identities", strings.NewReader(body))
	c.Assert(err, IsNil)
	cmd := apiCmd("/v1/identities")
	rsp, ok := cmd.POST(cmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)
	return rsp
}
