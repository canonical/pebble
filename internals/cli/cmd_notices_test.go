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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

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
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}, {
				"id": "2",
				"type": "warning",
				"key": "Ware!",
				"first-occurred": "2023-09-06T17:18:00Z",
				"last-occurred": "2023-09-06T19:18:00Z",
				"last-repeated": "2023-09-06T18:18:00Z",
				"occurrences": 1
			}
		]}`)
	})

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"notices", "--abs-time"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   Type     Key    First                 Last                  Repeated              Occurrences
1    custom   a.b/c  2023-09-05T17:18:00Z  2023-09-05T19:18:00Z  2023-09-05T18:18:00Z  3
2    warning  Ware!  2023-09-06T17:18:00Z  2023-09-06T19:18:00Z  2023-09-06T18:18:00Z  1
`[1:])
	c.Check(s.Stderr(), Equals, "")

	// Ensure that "last-listed" in notices.json is updated
	data, err := os.ReadFile(filename)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(data, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"last-listed": "2023-09-06T18:18:00Z",
		"last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) TestNoticesFilters(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"types": {"custom", "warning"},
			"keys":  {"a.b/c"},
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{
		"notices", "--abs-time", "--type", "custom", "--key", "a.b/c", "--type", "warning"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   Type    Key    First                 Last                  Repeated              Occurrences
1    custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T19:18:00Z  2023-09-05T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	// Ensure that "last-listed" in notices.json is updated
	data, err := os.ReadFile(filename)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(data, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"last-listed": "2023-09-05T18:18:00Z",
		"last-okayed": "0001-01-01T00:00:00Z",
	})
}

func (s *PebbleSuite) TestNoticesAfter(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/notices")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"after": {"2023-08-04T01:02:03Z"}, // from "last-okayed" in notices.json
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": [{
				"id": "1",
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-07T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	data := []byte(`{"last-listed": "2023-09-06T15:06:00Z", "last-okayed": "2023-08-04T01:02:03Z"}`)
	err := os.WriteFile(filename, data, 0600)
	c.Assert(err, IsNil)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"notices", "--abs-time"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   Type    Key    First                 Last                  Repeated              Occurrences
1    custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T19:18:00Z  2023-09-07T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	// Ensure that "last-listed" in notices.json is updated
	data, err = os.ReadFile(filename)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(data, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"last-listed": "2023-09-07T18:18:00Z",
		"last-okayed": "2023-08-04T01:02:03Z",
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

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"notices"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No matching notices.\n")

	// Shouldn't have updated notices.json
	_, err = os.Stat(filename)
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
				"type": "custom",
				"key": "a.b/c",
				"first-occurred": "2023-09-05T17:18:00Z",
				"last-occurred": "2023-09-05T19:18:00Z",
				"last-repeated": "2023-09-05T18:18:00Z",
				"occurrences": 3
			}
		]}`)
	})

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{
		"notices", "--abs-time", "--timeout", "1s"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
ID   Type    Key    First                 Last                  Repeated              Occurrences
1    custom  a.b/c  2023-09-05T17:18:00Z  2023-09-05T19:18:00Z  2023-09-05T18:18:00Z  3
`[1:])
	c.Check(s.Stderr(), Equals, "")

	// Ensure that "last-listed" in notices.json is updated
	data, err := os.ReadFile(filename)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(data, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"last-listed": "2023-09-05T18:18:00Z",
		"last-okayed": "0001-01-01T00:00:00Z",
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

	oldFilename := os.Getenv("PEBBLE_NOTICES_FILENAME")
	defer os.Setenv("PEBBLE_NOTICES_FILENAME", oldFilename)

	filename := filepath.Join(c.MkDir(), "notices.json")
	os.Setenv("PEBBLE_NOTICES_FILENAME", filename)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"notices", "--timeout", "1s"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No matching notices after waiting 1s.\n")

	// Shouldn't have updated notices.json
	_, err = os.Stat(filename)
	c.Assert(errors.Is(err, fs.ErrNotExist), Equals, true)
}
