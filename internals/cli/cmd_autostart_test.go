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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestAutostartExtraArgs(c *tc.C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"autostart", "extra", "args"})
	c.Assert(err, tc.Equals, cli.ErrExtraArgs)
	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestAutostart(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/42" {
			c.Check(r.Method, tc.Equals, "GET")
			fmt.Fprintf(w, `{
	"type": "sync",
	"result": {
		"id": "42",
		"kind": "autostart",
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

		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "42"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"autostart"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestAutostartFailsNoDefaultServices(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/services")
		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprint(w, `{
    "type": "error",
    "status-code": 400,
    "result": {"kind": "no-default-services", "message": "no default services"}
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"autostart"})
	c.Assert(err, tc.ErrorMatches, "no default services")
	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestAutostartNoWait(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/services")
		c.Check(r.URL.Path, tc.Not(tc.Equals), "/v1/changes/42")

		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "42"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"autostart", "--no-wait"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "42\n")
	c.Check(s.Stderr(), tc.Equals, ``)
}

func (s *PebbleSuite) TestAutostartFailsGetChange(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/changes/42" {
			c.Check(r.Method, tc.Equals, "GET")
			fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not foo"}}`)
			return
		}

		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/services")

		body := DecodedRequestBody(c, r)
		c.Check(body, tc.DeepEquals, map[string]any{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "42"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"autostart"})
	c.Assert(err, tc.ErrorMatches, "could not foo")
	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}
