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

package main_test

import (
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestReplanExtraArgs(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"replan", "extra", "args"})
	c.Assert(err, check.Equals, pebble.ErrExtraArgs)
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestReplan(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/43" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{
	"type": "sync",
	"result": {
		"id": "43",
		"kind": "replan",
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
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "replan",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "43"
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"replan"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestReplanFailsNoDefaultServices(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")
		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "replan",
			"services": nil,
		})

		fmt.Fprint(w, `{
    "type": "error",
    "status-code": 400,
    "result": {"kind": "no-default-services", "message": "no default services"}
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"replan"})
	c.Assert(err, check.ErrorMatches, "no default services")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestReplanNoWait(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")
		c.Check(r.URL.Path, check.Not(check.Equals), "/v1/changes/43")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "replan",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "43"
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"replan", "--no-wait"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "43\n")
	c.Check(s.Stderr(), check.Equals, ``)
}

func (s *PebbleSuite) TestReplanFailsGetChange(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/43" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not foo"}}`)
			return
		}

		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "replan",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "43"
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"replan"})
	c.Assert(err, check.ErrorMatches, "could not foo")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
