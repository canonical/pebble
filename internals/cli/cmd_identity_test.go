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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestIdentity(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
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
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
access: read
local:
    user-id: 42
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestIdentityJSON(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"bob": {"access": "read", "local": {"user-id": 42}}
			}
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identity", "--format", "json", "bob"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"access":"read","local":{"user-id":42}}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestIdentityYAML(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"bob": {"access": "read", "local": {"user-id": 42}}
			}
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identity", "--format", "yaml", "bob"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
access: read
local:
    user-id: 42
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestIdentityInvalidFormat(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"identity", "--format", "foobar", "bob"})
	c.Assert(err, tc.ErrorMatches, "Invalid value.*for option.*--format.*")
}

func (s *PebbleSuite) TestIdentityNotFound(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"identity", "foo"})
	c.Assert(err, tc.ErrorMatches, `cannot find identity "foo"`)
}
