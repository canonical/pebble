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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestRemoveIdentities(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)

	rest, err := cli.ParserForTest().ParseArgs([]string{"remove-identities", "--from", path})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "Removed 1 identity.\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestRemoveIdentitiesNotNull(c *tc.C) {
	path := filepath.Join(c.MkDir(), "identities.yaml")
	data := `
identities:
    bob:
        access: admin
        local: {user-id: 42}
`
	err := os.WriteFile(path, []byte(data), 0o666)
	c.Assert(err, tc.ErrorIsNil)

	_, err = cli.ParserForTest().ParseArgs([]string{"remove-identities", "--from", path})
	c.Assert(err, tc.ErrorMatches, `identity value for "bob" must be null for remove operation`)
}
