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
	pebble "github.com/canonical/pebble/cmd/pebble"
	"gopkg.in/check.v1"
	"net/http"
)

func (s *PebbleSuite) TestAutostart(c *check.C) {
	failGetChange := false
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fakeChange := `{
  "id":   "42",
  "kind": "autostart",
  "summary": "...",
  "status": "Done",
  "ready": true,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": []
}`
		if r.URL.Path == "/v1/changes" {
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintf(w, `{"type": "sync", "result": [%s]}`, fakeChange)
			return
		} else if r.URL.Path == "/v1/changes/42" {
			c.Check(r.Method, check.Equals, "GET")
			if failGetChange {
				fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not bar"}}`)
			} else {
				fmt.Fprintf(w, `{"type": "sync", "result": %s}`, fakeChange)
			}
			return
		}

		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")
		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprintf(w, `{
    "type": "async",
    "status-code": 202,
    "change": "42"
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"autostart"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	rest, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"autostart", "--no-wait"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	failGetChange = true
	rest, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"autostart"})
	c.Assert(err, check.ErrorMatches, "could not bar")
	c.Assert(rest, check.HasLen, 1)
	failGetChange = false

}

func (s *PebbleSuite) TestAutostartFails(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/services")
		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]interface{}{
			"action":   "autostart",
			"services": nil,
		})

		fmt.Fprint(w, `{
    "type": "error",
    "status-code": 400,
    "result": {"kind": "no-default-services", "message": "no default services"}
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"autostart"})
	c.Assert(err, check.ErrorMatches, "no default services")
	c.Assert(rest, check.HasLen, 1)
}

func (s *PebbleSuite) TestAutostartFailsWithExtraArgs(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"autostart", "extra", "args"})
	c.Assert(err, check.Equals, pebble.ErrExtraArgs)
	c.Assert(rest, check.HasLen, 1)
}
