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

package cli_test

import (
	"fmt"
	"net/http"
	"net/url"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
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
		{"name": "svc1", "current": "inactive", "startup": "enabled", "current-since": "2022-04-28T17:05:23+12:00"},
		{"name": "svc2", "current": "inactive", "startup": "enabled"},
		{"name": "svc3", "current": "backoff", "startup": "enabled"}
	]
}`)
	})
	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Service  Startup  Current   Since
svc1     enabled  inactive  2022-04-28
svc2     enabled  inactive  -
svc3     enabled  backoff   -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestPlanNoServices(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/services")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"names": {""}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "Plan has no services.\n")
}

func (s *PebbleSuite) TestNoMatchingServices(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/services")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"names": {"foo,bar"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"services", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "No matching services.\n")
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
		{"name": "bar", "current": "active", "startup": "disabled", "current-since": "2022-04-28T17:05:23+12:00"},
		{"name": "foo", "current": "inactive", "startup": "enabled"}
	]
}`)
	})
	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"services", "foo", "bar", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Service  Startup   Current   Since
bar      disabled  active    2022-04-28T17:05:23+12:00
foo      enabled   inactive  -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestServicesFail(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/services")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"names": {""}})
		fmt.Fprint(w, `{
    "type": "error",
    "result": {"message": "could not foo"}
}`)
	})
	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.ErrorMatches, "could not foo")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
