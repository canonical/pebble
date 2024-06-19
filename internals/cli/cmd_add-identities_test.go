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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestAddIdentitiesSingle(c *C) {
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
	c.Assert(err, IsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "Added 1 new identity.\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestAddIdentitiesMultiple(c *C) {
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
	c.Assert(err, IsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "Added 2 new identities.\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestAddIdentitiesUnmarshalError(c *C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	err := os.WriteFile(path, []byte("}not yaml{"), 0o666)
	c.Assert(err, IsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, ErrorMatches, `cannot unmarshal identities: .*`)
}

func (s *PebbleSuite) TestAddIdentitiesNoIdentities(c *C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
bob:
    access: admin
    local: {user-id: 42}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, IsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"add-identities", "--from", path})
	c.Assert(err, ErrorMatches, `no identities to add.*`)
}

func (s *PebbleSuite) checkPostIdentities(c *C, r *http.Request, action string, identities map[string]any) {
	c.Check(r.Method, Equals, "POST")
	c.Check(r.URL.Path, Equals, "/v1/identities")
	body, err := io.ReadAll(r.Body)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"action":     action,
		"identities": identities,
	})
}
