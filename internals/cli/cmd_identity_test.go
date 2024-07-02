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
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestIdentity(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/identities")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"bob": {"access": "read", "local": {"user-id": 42}},
				"mary": {"access": "admin", "local": {"user-id": 1000}}
			}
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identity", "bob"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
access: read
local:
    user-id: 42
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestIdentityNotFound(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/identities")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"identity", "foo"})
	c.Assert(err, ErrorMatches, `cannot find identity "foo"`)
}
