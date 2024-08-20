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
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestNotices(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}, {
				"id": "2",
				"user-id": null,
				"type": "warning",
				"key": "Ware!",
				"first-occurred": "2023-09-06T17:18:00Z",
				"last-occurred": "2023-09-06T19:18:00Z",
				"last-repeated": "2023-09-06T18:18:00Z",
				"occurrences": 1
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notices", "--abs-time"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   User    Type     Key    First                 Repeated              Occurrences
1    1000    custom   a.b/c  2023-09-05T17:18:00Z  2023-09-05T18:18:00Z  3
2    public  warning  Ware!  2023-09-06T17:18:00Z  2023-09-06T18:18:00Z  1
`[1:])
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-06T18:18:00Z",
		"notices-last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) readNoticesCLIState(c *C) map[string]any {
	fullCLIState := s.readCLIState(c)
	cliState := map[string]any{
		"notices-last-listed": fullCLIState["notices-last-listed"],
		"notices-last-okayed": fullCLIState["notices-last-okayed"],
	}
	return cliState
}

func (s *PebbleSuite) TestNoticesFiltersUsers(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"users": {"all"},
			"types": {"custom", "warning"},
			"keys":  {"a.b/c"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{
		"notices", "--abs-time", "--users", "all", "--type", "custom", "--key", "a.b/c", "--type", "warning"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   User  Type    Key    First                 Repeated              Occurrences
1    1000  custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-05T18:18:00Z",
		"notices-last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) TestNoticesFiltersUID(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"user-id": {"1000"},
			"types":   {"custom", "warning"},
			"keys":    {"a.b/c"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{
		"notices", "--abs-time", "--uid", "1000", "--type", "custom", "--key", "a.b/c", "--type", "warning"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   User  Type    Key    First                 Repeated              Occurrences
1    1000  custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-05T18:18:00Z",
		"notices-last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) TestNoticesAfter(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"after": {"2023-08-04T01:02:03Z"}, // from "notices-last-okayed" in notices.json
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-07T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	s.writeCLIState(c, map[string]any{
		"notices-last-listed": time.Date(2023, 9, 6, 15, 6, 0, 0, time.UTC),
		"notices-last-okayed": time.Date(2023, 8, 4, 1, 2, 3, 0, time.UTC),
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notices", "--abs-time"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   User  Type    Key    First                 Repeated              Occurrences
1    1000  custom  a.b/c  2023-09-05T17:18:00Z  2023-09-07T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-07T18:18:00Z",
		"notices-last-okayed": "2023-08-04T01:02:03Z",
	})
}

func (s *PebbleSuite) TestNoticesNoNotices(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": []}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notices"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No matching notices.\n")

	// Shouldn't have updated cli.json
	_, err = os.Stat(s.cliStatePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
}

func (s *PebbleSuite) TestNoticesTimeout(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{"timeout": {"1s"}})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"user-id": 1000,
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{
		"notices", "--abs-time", "--timeout", "1s"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   User  Type    Key    First                 Repeated              Occurrences
1    1000  custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-05T18:18:00Z",
		"notices-last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) TestNoticesNoNoticesTimeout(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{"timeout": {"1s"}})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": []}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"notices", "--timeout", "1s"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No matching notices after waiting 1s.\n")

	// Shouldn't have updated cli.json
	_, err = os.Stat(s.cliStatePath)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
}
