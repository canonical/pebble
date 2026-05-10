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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestSignalShortName(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/signals")
		c.Check(body, tc.DeepEquals, map[string]any{
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
}

func (s *PebbleSuite) TestSignalFullName(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/signals")
		c.Check(body, tc.DeepEquals, map[string]any{
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
}

func (s *PebbleSuite) TestSignalMultipleServices(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/signals")
		c.Check(body, tc.DeepEquals, map[string]any{
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
}

func (s *PebbleSuite) TestSignalErrorLowercase(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"signal", "hup", "s1"})
	c.Assert(err, tc.ErrorMatches, "signal name must be uppercase, for example HUP")
}

func (s *PebbleSuite) TestSignalServerError(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		body := DecodedRequestBody(c, r)
		c.Check(r.Method, tc.Equals, "POST")
		c.Check(r.URL.Path, tc.Equals, "/v1/signals")
		c.Check(body, tc.DeepEquals, map[string]any{
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
	c.Assert(err, tc.ErrorMatches, `invalid signal name "SIGFOO"`)
}
