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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestStopChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/25" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{
 	"type": "sync",
 	"result": {
 		"id": "25",
 		"kind": "stop",
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
		c.Check(r.URL.Path, check.Equals, "/v1/checks")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action": "stop",
			"checks": []interface{}{"chk1", "chk2"},
		})

		fmt.Fprintf(w, `{
     "type": "async",
     "status-code": 202,
     "change": "25"
 }`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"stop-checks", "chk1", "chk2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestStopChecksFails(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/checks")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action": "stop",
			"checks": []interface{}{"chk1", "chk3"},
		})

		fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not foo"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"stop-checks", "chk1", "chk3"})
	c.Assert(err, check.ErrorMatches, "could not foo")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestStopChecksNoWait(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/checks")
		c.Check(r.URL.Path, check.Not(check.Equals), "/v1/changes/25")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action": "stop",
			"checks": []interface{}{"chk1", "chk2"},
		})

		fmt.Fprintf(w, `{
     "type": "async",
     "status-code": 202,
     "change": "25"
 }`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"stop-checks", "chk1", "chk2", "--no-wait"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "46\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestStopChecksFailsGetChange(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/25" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not bar"}}`)
			return
		}

		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/checks")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action": "stop",
			"checks": []interface{}{"chk1", "chk2"},
		})

		fmt.Fprintf(w, `{
     "type": "async",
     "status-code": 202,
     "change": "25"
 }`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"stop-checks", "chk1", "chk2"})
	c.Assert(err, check.ErrorMatches, "could not bar")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
