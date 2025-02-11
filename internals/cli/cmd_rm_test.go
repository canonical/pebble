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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestRmExtraArgs(c *C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"rm", "extra", "args"})
	c.Assert(err, Equals, cli.ErrExtraArgs)
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestRm(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "remove",
			"paths": []any{
				map[string]any{
					"path":      "/foo/bar.baz",
					"recursive": false,
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar.baz"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"rm", "/foo/bar.baz"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestRmRecursive(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "remove",
			"paths": []any{
				map[string]any{
					"path":      "/foo/bar",
					"recursive": true,
				},
			},
		})

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/foo/bar"}]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"rm", "-r", "/foo/bar"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestRmFails(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "remove",
			"paths": []any{
				map[string]any{
					"path":      "/foo/bar.baz",
					"recursive": false,
				},
			},
		})

		fmt.Fprintln(w, `{"type": "error", "result": {"message": "could not foo"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"rm", "/foo/bar.baz"})
	c.Assert(err, ErrorMatches, "could not foo")
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestRmFailsOnPath(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/files")

		body := DecodedRequestBody(c, r)
		c.Check(body, DeepEquals, map[string]any{
			"action": "remove",
			"paths": []any{
				map[string]any{
					"path":      "/foo/bar",
					"recursive": true,
				},
			},
		})

		fmt.Fprintln(w, ` {
			"type": "sync",
			"result": [{
				"path": "/foo/bar/baz.qux",
				"error": {
					"message": "could not baz",
					"kind": "permission-denied",
					"value": 42
				}
			}]
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"rm", "-r", "/foo/bar"})

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not baz")
	c.Assert(clientErr.Kind, Equals, "permission-denied")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
