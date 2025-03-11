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
	"encoding/json"
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestMkdirExtraArgs(c *C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "/foo", "extra", "args"})
	c.Assert(err, Equals, cli.ErrExtraArgs)
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdir(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo",
					"make-parents": false,
					"permissions":  "",
					"user-id":      nil,
					"user":         "",
					"group-id":     nil,
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "/foo"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirFailsParsingPermissions(c *C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "-m", "foobar", "/foo"})
	c.Assert(err, ErrorMatches, `invalid mode for directory: "foobar"`)
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirMakeParents(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo/bar",
					"make-parents": true,
					"permissions":  "",
					"user-id":      nil,
					"user":         "",
					"group-id":     nil,
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "-p", "/foo/bar"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirPermissions(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo/bar",
					"make-parents": false,
					"permissions":  "755",
					"user-id":      nil,
					"user":         "",
					"group-id":     nil,
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "-m", "755", "/foo/bar"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirOwnerIDs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo/bar",
					"make-parents": false,
					"permissions":  "",
					"user-id":      json.Number("1000"),
					"user":         "",
					"group-id":     json.Number("1000"),
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "--uid", "1000", "--gid", "1000", "/foo/bar"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirOwnerNames(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo/bar",
					"make-parents": false,
					"permissions":  "",
					"user-id":      nil,
					"user":         "root",
					"group-id":     nil,
					"group":        "wheel",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "--user", "root", "--group", "wheel", "/foo/bar"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirFails(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foo",
					"make-parents": false,
					"permissions":  "",
					"user-id":      nil,
					"user":         "",
					"group-id":     nil,
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, `{"type": "error", "result": {"message": "could not foo"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "/foo"})
	c.Assert(err, ErrorMatches, "could not foo")
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestMkdirFailsOnDirectory(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "make-dirs",
			"dirs": []any{
				map[string]any{
					"path":         "/foobar",
					"make-parents": false,
					"permissions":  "",
					"user-id":      nil,
					"user":         "",
					"group-id":     nil,
					"group":        "",
				},
			},
		})

		fmt.Fprintln(w, ` {
			"type": "sync",
			"result": [{
				"path": "/foobar",
				"error": {
					"message": "could not bar",
					"kind": "permission-denied",
					"value": 42
				}
			}]
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"mkdir", "/foobar"})

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not bar")
	c.Assert(clientErr.Kind, Equals, "permission-denied")

	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
