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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestCheck(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk1"}})
		fmt.Fprint(w, `
{
    "type": "sync",
    "status-code": 200,
    "result": [{"name": "chk1", "startup": "enabled", "status": "up", "successes": 5, "threshold": 3, "change-id": "1"}]
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: up
successes: 5
failures: 0
threshold: 3
change-id: "1"
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckFailure(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/checks":
			c.Assert(r.Method, tc.Equals, "GET")
			c.Assert(r.URL.Path, tc.Equals, "/v1/checks")
			c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk1"}})
			fmt.Fprint(w, `
{
	"type": "sync",
	"status-code": 200,
	"result": [{"name": "chk1", "startup": "enabled", "status": "up", "failures": 1, "threshold": 3, "change-id": "1"}]
}`)
		case "/v1/changes/1":
			fmt.Fprint(w, `
{
"type": "sync",
"result": {
"id": "2",
"kind": "perform-check",
"status": "Doing",
"tasks": [{"kind": "perform-check", "status": "Doing", "log": ["2025-02-27T17:06:57Z ERROR"]}]
}
}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: up
failures: 1
threshold: 3
change-id: "1"
logs: |
    2025-02-27T17:06:57Z ERROR
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckNotFound(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk2"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": []
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk2"})
	c.Assert(err, tc.NotNil)
	c.Assert(rest, tc.HasLen, 1)
	c.Check(err, tc.ErrorMatches, "cannot find check .*")
}

func (s *PebbleSuite) TestCheckRefresh(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "POST")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks/refresh")
		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"name": "chk1",
		})
		fmt.Fprint(w, `
{
    "type": "sync",
    "status-code": 200,
    "result": {
        "info": {"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3, "change-id": "1"}
	}
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "--refresh", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: up
failures: 0
threshold: 3
change-id: "1"
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckRefreshFailure(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/checks/refresh":
			c.Assert(r.Method, tc.Equals, "POST")
			c.Assert(r.URL.Path, tc.Equals, "/v1/checks/refresh")
			body := DecodedRequestBody(c, r)
			c.Check(body, tc.DeepEquals, map[string]any{
				"name": "chk1",
			})
			fmt.Fprint(w, `
{
	"type": "sync",
	"status-code": 200,
	"result": {
		"info": {"name": "chk1", "startup": "enabled", "status": "up", "threshold": 3, "change-id": "1"},
		"error": "somme error"
	}
}`)
		case "/v1/changes/1":
			fmt.Fprint(w, `
{
	"type": "sync",
	"result": {
		"id": "2",
		"kind": "perform-check",
		"status": "Doing",
		"tasks": [{"kind": "perform-check", "status": "Doing", "log": ["2025-02-27T17:06:57Z ERROR"]}]
	}
}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "--refresh", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: up
failures: 0
threshold: 3
change-id: "1"
error: somme error
logs: |
    2025-02-27T17:06:57Z ERROR
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckRefreshNotFound(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "POST")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks/refresh")
		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"name": "chk1",
		})
		fmt.Fprint(w, `{
    "type": "error",
    "status-code": 404,
	"status": "tc.Not Found",
    "result": {
        "message": "cannot find check with name \"chk1\""
	}
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "--refresh", "chk1"})
	c.Assert(err, tc.NotNil)
	c.Assert(rest, tc.HasLen, 1)
	c.Check(err, tc.ErrorMatches, "cannot find check .*")
}

func (s *PebbleSuite) TestCheckJSON(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk1"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [{"name": "chk1", "startup": "enabled", "status": "up", "successes": 5, "threshold": 3, "change-id": "1"}]
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "--format", "json", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"name":"chk1","startup":"enabled","status":"up","successes":5,"failures":0,"threshold":3,"change-id":"1"}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckYAML(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk1"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [{"name": "chk1", "startup": "enabled", "status": "up", "successes": 5, "threshold": 3, "change-id": "1"}]
}`)
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "--format", "yaml", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: up
successes: 5
failures: 0
threshold: 3
change-id: "1"
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestCheckInvalidFormat(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"check", "--format", "foobar", "chk1"})
	c.Assert(err, tc.ErrorMatches, "Invalid value.*for option.*--format.*")
}

func (s *PebbleSuite) TestCheckPrevChangeLog(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/checks":
			c.Assert(r.Method, tc.Equals, "GET")
			c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"names": {"chk1"}})
			fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [{"name": "chk1", "startup": "enabled", "status": "down", "failures": 3, "threshold": 3, "change-id": "2", "prev-change-id": "1"}]
}`)
		case "/v1/changes/2":
			fmt.Fprint(w, `{
	"type": "sync",
	"result": {"id": "2", "kind": "recover-check", "status": "Doing", "tasks": [{"kind": "recover-check", "status": "Doing", "log": []}]}
}`)
		case "/v1/changes/1":
			fmt.Fprint(w, `{
	"type": "sync",
	"result": {"id": "1", "kind": "perform-check", "status": "Error", "tasks": [{"kind": "perform-check", "status": "Error", "log": ["2024-04-18T12:16:57Z ERROR connection refused"]}]}
}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"check", "chk1"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
name: chk1
startup: enabled
status: down
failures: 3
threshold: 3
change-id: "2"
prev-change-id: "1"
logs: |
    2024-04-18T12:16:57Z ERROR connection refused
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}
