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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestCheck(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/checks")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"names": {"chk1"}})
		fmt.Fprint(w, `
{
    "type": "sync",
    "status-code": 200,
    "result": [{"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3, "change-id": "1"}]
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk1"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
name: chk1
startup: enabled
status: up
failures: 0
threshold: 3
change-id: "1"
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestCheckNotFound(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/checks")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"names": {"chk2"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk2"})
	c.Assert(err, NotNil)
	c.Assert(rest, HasLen, 1)
	c.Check(err, ErrorMatches, "cannot find check .*")
}
