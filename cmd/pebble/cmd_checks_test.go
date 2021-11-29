// Copyright (c) 2021 Canonical Ltd
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

package main_test

import (
	"fmt"
	"net/http"
	"net/url"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"level": {""}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "healthy": true},
		{"name": "chk2", "healthy": false, "failures": 1, "last-error": "ERROR!"},
		{"name": "chk3", "healthy": false, "failures": 42, "last-error": "big error", "error-details": "details..."}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Healthy  Failures  Last Error
chk1          true     0         
chk2          false    1         ERROR!
chk3          false    42        big error
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChecksFiltering(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"level": {"alive"}, "names": {"chk1", "chk3"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "healthy": true},
		{"name": "chk3", "healthy": false, "failures": 42, "last-error": "big error", "error-details": "details..."}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks", "--level=alive", "chk1", "chk3"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Healthy  Failures  Last Error
chk1          true     0         
chk3          false    42        big error
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}
