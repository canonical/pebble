// Copyright (c) 2022 Canonical Ltd
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

func (s *PebbleSuite) TestChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		switch r.URL.Path {
		case "/v1/checks":
			fmt.Fprint(w, `
{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3, "change-id": "1"},
		{"name": "chk2", "startup": "enabled", "status": "down", "failures": 1, "threshold": 1, "change-id": "2"},
		{"name": "chk3", "startup": "enabled", "level": "alive", "status": "down", "failures": 42, "threshold": 3, "change-id": "3"},
		{"name": "chk4", "startup": "enabled", "status": "down", "failures": 6, "threshold": 2, "change-id": "4"},
		{"name": "chk5", "startup": "enabled", "status": "down", "failures": 3, "threshold": 2, "change-id": "5"},
		{"name": "chk6", "startup": "disabled", "status": "inactive", "failures": 0, "threshold": 3, "change-id": ""}
	]
}`)
		case "/v1/changes/2":
			fmt.Fprint(w, `
{
	"type": "sync",
	"result": {
		"id": "2",
		"kind": "recover-check",
		"status": "Doing",
		"tasks": [{"kind": "recover-check", "status": "Doing", "log": ["first", "2024-04-18T12:16:57Z ERROR second"]}]
	}
}`)
		case "/v1/changes/3":
			fmt.Fprint(w, `
{
	"type": "error",
	"result": {"message": "cannot get change 3"}
}`)
		case "/v1/changes/4":
			fmt.Fprint(w, `
{
	"type": "sync",
	"result": {
		"id": "4",
		"kind": "recover-check",
		"status": "Doing",
		"tasks": [{"kind": "recover-check", "status": "Doing", "log": ["2024-04-18T12:16:57+12:00 ERROR Get \"http://localhost:8000/\": dial tcp 127.0.0.1:8000: connect: connection refused"]}]
	}
}`)
		case "/v1/changes/5":
			fmt.Fprint(w, `
{
	"type": "sync",
	"result": {
		"id": "5",
		"kind": "perform-check",
		"status": "Doing",
		"tasks": [{"kind": "recover-check", "status": "Doing", "log": ["2024-04-18T12:16:57+12:00 ERROR error with some\\nline breaks\\nin it\\n"]}]
	}
}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"checks"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Startup   Status    Failures  Change
chk1   -      enabled   up        0/3       1
chk2   -      enabled   down      1/1       2 (second)
chk3   alive  enabled   down      42/3      3 (cannot get change 3)
chk4   -      enabled   down      6/2       4 (Get "http://localhost:8000/": dial tc... run "pebble tasks 4" for more)
chk5   -      enabled   down      3/2       5 (error with some\nline breaks\nin it\n)
chk6   -      disabled  inactive  -         -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestPlanNoChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"checks"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "Plan has no health checks.\n")
}

func (s *PebbleSuite) TestNoMatchingChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"level": {"alive"}, "names": {"chk1", "chk3"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"checks", "--level=alive", "chk1", "chk3"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "No matching health checks.\n")
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
		{"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3},
		{"name": "chk3", "startup": "enabled", "level": "alive", "status": "down", "failures": 42, "threshold": 3}
	]
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"checks", "--level=alive", "chk1", "chk3"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Startup  Status  Failures  Change
chk1   -      enabled  up      0/3       -
chk3   alive  enabled  down    42/3      -
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChecksFails(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
    "type": "error",
    "result": {"message": "could not bar"}
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"checks"})
	c.Assert(err, check.ErrorMatches, "could not bar")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
