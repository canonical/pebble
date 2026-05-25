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

package client_test

import (
	"encoding/json"
	"io"
	"net/url"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestIdentities(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
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
	}}`
	identities, err := cs.cli.Identities(nil)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(cs.req.Method, tc.Equals, "GET")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/identities")
	c.Assert(cs.req.URL.Query(), tc.DeepEquals, url.Values{})
	c.Assert(identities, tc.DeepEquals, map[string]*client.Identity{
		"bob": {
			Access: client.ReadAccess,
			Local:  &client.LocalIdentity{UserID: new(uint32(42))},
		},
		"mary": {
			Access: client.AdminAccess,
			Local:  &client.LocalIdentity{UserID: new(uint32(1000))},
		},
	})
}

func (cs *clientSuite) TestIdentitiesNullIdentity(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
		"bob": null
	}}`
	identities, err := cs.cli.Identities(nil)
	c.Assert(err, tc.ErrorMatches, `server returned null identity "bob"`)
	c.Assert(cs.req.Method, tc.Equals, "GET")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/identities")
	c.Assert(cs.req.URL.Query(), tc.DeepEquals, url.Values{})
	c.Assert(identities, tc.IsNil)
}

func (cs *clientSuite) TestAddIdentities(c *tc.C) {
	cs.testPostIdentities(c, "add", cs.cli.AddIdentities)
}

func (cs *clientSuite) TestUpdateIdentities(c *tc.C) {
	cs.testPostIdentities(c, "update", cs.cli.UpdateIdentities)
}

func (cs *clientSuite) TestReplaceIdentities(c *tc.C) {
	cs.testPostIdentities(c, "replace", cs.cli.ReplaceIdentities)
}

func (cs *clientSuite) TestRemoveIdentities(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": null}`
	err := cs.cli.RemoveIdentities(map[string]struct{}{
		"bob":  {},
		"mary": {},
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(cs.req.Method, tc.Equals, "POST")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/identities")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, tc.ErrorIsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(m, tc.DeepEquals, map[string]any{
		"action": "remove",
		"identities": map[string]any{
			"bob":  nil,
			"mary": nil,
		},
	})
}

func (cs *clientSuite) testPostIdentities(c *tc.C, action string, clientFunc func(map[string]*client.Identity) error) {
	cs.rsp = `{"type": "sync", "result": null}`
	err := clientFunc(map[string]*client.Identity{
		"bob": {
			Access: client.ReadAccess,
			Local:  &client.LocalIdentity{UserID: new(uint32(42))},
		},
		"mary": {
			Access: client.AdminAccess,
			Local:  &client.LocalIdentity{UserID: new(uint32(1000))},
		},
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(cs.req.Method, tc.Equals, "POST")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/identities")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, tc.ErrorIsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(m, tc.DeepEquals, map[string]any{
		"action": action,
		"identities": map[string]any{
			"bob": map[string]any{
				"access": "read",
				"local": map[string]any{
					"user-id": 42.0,
				},
			},
			"mary": map[string]any{
				"access": "admin",
				"local": map[string]any{
					"user-id": 1000.0,
				},
			},
		},
	})
}
