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

func (s *PebbleSuite) TestServices(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/services")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"names": {""}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "svc1", "status": "inactive", "default": "start"}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Service  Startup  Current   Notes
svc1     enabled  inactive  -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestServicesNames(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/services")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"names": {"foo,bar"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "bar", "status": "active", "default": "stop"},
		{"name": "foo", "status": "inactive", "default": "start"}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"services", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Service  Startup   Current   Notes
bar      disabled  active    -
foo      enabled   inactive  -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}
