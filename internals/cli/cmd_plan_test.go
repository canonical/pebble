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
	"net/url"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestGetPlan(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/plan")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"format": []string{"yaml"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": "services:\n    foo:\n        override: replace\n        command: cmd\n"
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"plan"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Assert(s.Stdout(), tc.Equals, `
services:
    foo:
        override: replace
        command: cmd
`[1:])
	c.Assert(s.Stderr(), tc.Equals, ``)
}

func (s *PebbleSuite) TestGetPlanFails(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/plan")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"format": []string{"yaml"}})
		fmt.Fprint(w, `{
    "type": "error",
    "result": {"message": "could not do the thing"}
}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"plan"})
	c.Assert(err.Error(), tc.Equals, "could not do the thing")
	c.Assert(rest, tc.HasLen, 1)
	c.Assert(s.Stdout(), tc.Equals, ``)
	c.Assert(s.Stderr(), tc.Equals, ``)
}

func (s *PebbleSuite) TestPlanExtraArgs(c *tc.C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"plan", "extra", "args"})
	c.Assert(err, tc.Equals, cli.ErrExtraArgs)
	c.Check(rest, tc.HasLen, 1)
}
