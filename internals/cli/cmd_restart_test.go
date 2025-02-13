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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestRestart(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/44" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{
	"type": "sync",
	"result": {
		"id": "44",
		"kind": "restart",
		"summary": "...",
		"status": "Done",
		"ready": true,
		"spawn-time": "2016-04-21T01:02:03Z",
		"ready-time": "2016-04-21T01:02:04Z",
		"tasks": []
	}
}`)
			return
		}

		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action":   "restart",
			"services": []any{"srv1", "srv2"},
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "44"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"restart", "srv1", "srv2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestRestartFails(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action":   "restart",
			"services": []any{"srv1", "srv3"},
		})

		fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not foo"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"restart", "srv1", "srv3"})
	c.Assert(err, check.ErrorMatches, "could not foo")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestRestartNoWait(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")
		c.Check(r.URL.Path, check.Not(check.Equals), "/v1/changes/44")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action":   "restart",
			"services": []any{"srv1", "srv2"},
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "44"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"restart", "srv1", "srv2", "--no-wait"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "44\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestRestartFailsGetChange(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/44" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not bar"}}`)
			return
		}

		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action":   "restart",
			"services": []any{"srv1", "srv2"},
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "44"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"restart", "srv1", "srv2"})
	c.Assert(err, check.ErrorMatches, "could not bar")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
