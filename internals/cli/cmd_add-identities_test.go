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

package cli_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestAddIdentitiesSingle(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		s.checkPostIdentities(c, r, "add", map[string]any{
			"bob": map[string]any{
				"access": "admin",
				"local": map[string]any{
					"user-id": 42.0,
				},
			},
		})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": null
		}`)
	})

	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
identities:
    bob:
        access: admin
        local: {user-id: 42}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, tc.ErrorIsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "Added 1 new identity.\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestAddIdentitiesMultiple(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		s.checkPostIdentities(c, r, "add", map[string]any{
			"bob": map[string]any{
				"access": "admin",
				"local": map[string]any{
					"user-id": 42.0,
				},
			},
			"mary": map[string]any{
				"access": "read",
				"local": map[string]any{
					"user-id": 1000.0,
				},
			},
		})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": null
		}`)
	})

	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
identities:
    bob:
        access: admin
        local: {user-id: 42}
    mary:
        access: read
        local: {user-id: 1000}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, tc.ErrorIsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "Added 2 new identities.\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestAddIdentitiesUnmarshalError(c *tc.C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	err := os.WriteFile(path, []byte("}not yaml{"), 0o666)
	c.Assert(err, tc.ErrorIsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, tc.ErrorMatches, `cannot unmarshal identities: .*`)
}

func (s *PebbleSuite) TestAddIdentitiesNoIdentities(c *tc.C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
bob:
    access: admin
    local: {user-id: 42}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, tc.ErrorIsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, tc.ErrorMatches, `no identities to add.*`)
}

func (s *PebbleSuite) checkPostIdentities(c *tc.C, r *http.Request, action string, identities map[string]any) {
	c.Check(r.Method, tc.Equals, "POST")
	c.Check(r.URL.Path, tc.Equals, "/v1/identities")
	body, err := io.ReadAll(r.Body)
	c.Assert(err, tc.ErrorIsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(m, tc.DeepEquals, map[string]any{
		"action":     action,
		"identities": identities,
	})
}
