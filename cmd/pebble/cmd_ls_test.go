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

func (s *PebbleSuite) TestLsDirectory(c *C) {
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

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "-d", "/"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)

	c.Check(s.Stdout(), Equals, "/\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLsLongFormat(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"action": {"list"}, "path": {"/"}})
		fmt.Fprintln(w, `{
	"type": "sync",
	"result": [
		{
			"path": "/foo",
			"name": "foo",
			"type": "directory",
			"permissions": "777",
			"last-modified": "2016-04-21T01:02:03Z",
			"user-id": 0,
			"user": "root",
			"group-id": 0,
			"group": "root"
		},
		{
			"path": "/bar",
			"name": "bar",
			"type": "file",
			"permissions": "000",
			"last-modified": "2021-04-21T01:02:03Z",
			"user-id": 600,
			"user": "toor",
			"group-id": 600,
			"group": "toor",
			"size": 1000000000
		}
	]
}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "-l", "/"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)

	c.Check(s.Stdout(), Matches, `(?ms)drwxrwxrwx +root +root +- +2016-04-21 +foo
---------- +toor +toor +1.00GB +2021-04-21 +bar
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLsFails(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"path": {"/"}, "action": {"list"}, "itself": {"true"}})
		fmt.Fprintln(w, `{"type":"error","result":{"message":"could not foo"}}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "-d", "/"})
	c.Assert(err, ErrorMatches, "could not foo")
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLsPattern(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{"path": {"/foo/"}, "action": {"list"}, "pattern": {"bar.*"}})
		fmt.Fprintln(w, `{"type":"sync","result":[]}`)
	})

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "/foo/bar.*"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLsInvalidPattern(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"ls", "/foo/ba[rz]/fail"})
	c.Assert(err, ErrorMatches, "can only use globs on the last path element")
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
