// Copyright (c) 2023 Canonical Ltd
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

func (s *PebbleSuite) TestNoticeID(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/notices/123")

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"id": "123",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 1,
				"last-data": {"k": "v"},
				"repeat-after": "1h0m0s",
				"expire-after": "168h0m0s"
			}
		}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notice", "123"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
id: "123"
user-id: 1000
type: custom
key: a.b/c
first-occurred: 2023-09-05T17:18:00Z
last-occurred: 2023-09-05T19:18:00Z
last-repeated: 2023-09-05T18:18:00Z
occurrences: 1
last-data:
    k: v
repeat-after: 1h0m0s
expire-after: 168h0m0s
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoticeIDNotFound(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/notices/123")

		fmt.Fprint(w, `{
			"type": "error",
			"status-code": 404,
			"result": {
				"message": "cannot find notice with ID \"123\""
			}
		}`)
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"notice", "123"})
	c.Assert(err, tc.ErrorMatches, `cannot find notice with ID "123"`)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoticeTypeKey(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/notices")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{
			"types": {"custom"},
			"keys":  {"a.b/c"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "123",
				"user-id": null,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 1
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notice", "custom", "a.b/c"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
id: "123"
user-id: null
type: custom
key: a.b/c
first-occurred: 2023-09-05T17:18:00Z
last-occurred: 2023-09-05T19:18:00Z
last-repeated: 2023-09-05T18:18:00Z
occurrences: 1
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoticeTypeKeyUID(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/notices")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{
			"types":   {"custom"},
			"keys":    {"a.b/c"},
			"user-id": {"1001"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "123",
				"user-id": 1001,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 1
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notice", "--uid", "1001", "custom", "a.b/c"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
id: "123"
user-id: 1001
type: custom
key: a.b/c
first-occurred: 2023-09-05T17:18:00Z
last-occurred: 2023-09-05T19:18:00Z
last-repeated: 2023-09-05T18:18:00Z
occurrences: 1
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoticeTypeKeyNotFound(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/notices")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{
			"types": {"custom"},
			"keys":  {"a.b/c"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": []}`)
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"notice", "custom", "a.b/c"})
	c.Assert(err, tc.ErrorMatches, `cannot find custom notice with key "a.b/c"`)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}
