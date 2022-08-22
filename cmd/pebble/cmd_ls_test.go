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
	"net/url"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestLsExtraArgs(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "extra", "args"})
	c.Assert(err, Equals, pebble.ErrExtraArgs)
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLsItself(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"action": {"list"}, "path": {"/"}, "itself": {"true"}})
		fmt.Fprintln(w, `{
	"type": "sync",
	"result": [{
		"path": "/",
		"name": "/",
		"type": "directory",
		"permissions": "777",
		"last-modified": "2016-04-21T01:02:03Z",
		"user-id": 0,
		"user": "root",
		"group-id": 0,
		"group": "root"
	}]
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "--itself", "/"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
}
