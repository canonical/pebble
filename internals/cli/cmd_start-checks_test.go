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

func (s *PebbleSuite) TestStartChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/checks")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action": "start",
			"checks": []any{"chk1", "chk2", "chk3"},
		})

		fmt.Fprintf(w, `{
     "type": "sync",
     "status-code": 200,
	 "result": {"changed": ["chk1", "chk2"]}
 }`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"start-checks", "chk1", "chk2", "chk3"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "Checks started: chk1, chk2\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestStartChecksFails(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/checks")

		body := DecodedRequestBody(c, r)
		c.Check(body, check.DeepEquals, map[string]any{
			"action": "start",
			"checks": []any{"chk1", "chk3"},
		})

		fmt.Fprintf(w, `{"type": "error", "result": {"message": "could not foo"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"start-checks", "chk1", "chk3"})
	c.Assert(err, check.ErrorMatches, "could not foo")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
