// Copyright (c) 2025 Canonical Ltd
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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestCheck(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/check")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"name": {"chk1"}})
		fmt.Fprint(w, `
{
    "type": "sync",
    "status-code": 200,
    "result": {"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3, "change-id": "1"}
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk1"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Startup  Status  Failures  Change
chk1   -      enabled  up      0/3       1
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestCheckNotFound(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/check")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"name": {"chk2"}})
		fmt.Fprint(w, `{
    "type":"error",
	"status-code":404,
	"status":"Not Found",
	"result":{"message":"cannot find check with name \"chk1\""}}
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk2"})
	c.Assert(err, check.ErrorMatches, "cannot find check with name \"chk1\"")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
