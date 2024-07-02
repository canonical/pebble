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
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestRemoveIdentities(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		s.checkPostIdentities(c, r, "remove", map[string]any{
			"bob": nil,
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
    bob: null
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, IsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"remove-identities", "--from", path})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "Removed 1 identity.\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestRemoveIdentitiesNotNull(c *C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
identities:
    bob:
        access: admin
        local: {user-id: 42}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, IsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"remove-identities", "--from", path})
	c.Assert(err, ErrorMatches, `identity value for "bob" must be null for remove operation`)
}
