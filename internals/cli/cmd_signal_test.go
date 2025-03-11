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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestSignalShortName(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/signals")
		c.Check(body, check.DeepEquals, map[string]any{
			"signal":   "SIGHUP",
			"services": []any{"s1"},
		})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": true
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"signal", "HUP", "s1"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
}

func (s *PebbleSuite) TestSignalFullName(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/signals")
		c.Check(body, check.DeepEquals, map[string]any{
			"signal":   "SIGHUP",
			"services": []any{"s2"},
		})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": true
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"signal", "SIGHUP", "s2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
}

func (s *PebbleSuite) TestSignalMultipleServices(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/signals")
		c.Check(body, check.DeepEquals, map[string]any{
			"signal":   "SIGHUP",
			"services": []any{"s1", "s2"},
		})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": true
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"signal", "SIGHUP", "s1", "s2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
}

func (s *PebbleSuite) TestSignalErrorLowercase(c *check.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"signal", "hup", "s1"})
	c.Assert(err, check.ErrorMatches, "signal name must be uppercase, for example HUP")
}

func (s *PebbleSuite) TestSignalServerError(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/signals")
		c.Check(body, check.DeepEquals, map[string]any{
			"signal":   "SIGHUP",
			"services": []any{"s1"},
		})
		fmt.Fprint(w, `{
			"type": "error",
			"status-code": 400,
			"status": "Bad Request",
			"result": {"message":"invalid signal name \"SIGFOO\""}
		}`)
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"signal", "HUP", "s1"})
	c.Assert(err, check.ErrorMatches, `invalid signal name "SIGFOO"`)
}
