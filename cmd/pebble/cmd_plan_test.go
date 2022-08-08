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

package main_test

import (
	"fmt"
	"net/http"
	"net/url"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestGetPlan(c *check.C) {
	fail := false

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/v1/plan")
		c.Check(r.URL.Query(), check.DeepEquals, url.Values{"format": []string{"yaml"}})

		if fail {
			fmt.Fprint(w, `{
    "type": "error",
    "result": {"message": "could not do the thing"}
}`)
		} else {
			fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": "services:\n    foo:\n        override: replace\n        command: cmd\n"
}`)
		}
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"plan"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
services:
    foo:
        override: replace
        command: cmd
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
	s.ResetStdStreams()

	fail = true
	rest, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"plan"})
	c.Assert(err.Error(), check.Equals, "could not do the thing")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestExtraArguments(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"plan", "extra", "args"})
	c.Assert(err, check.Equals, pebble.ErrExtraArgs)
	c.Check(rest, check.HasLen, 1)
}
