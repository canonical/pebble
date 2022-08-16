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

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestLogsText(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/logs")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"n": []string{"30"},
		})
		fmt.Fprintf(w, `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1"}
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two"}
{"time":"2021-05-03T03:55:50.076800988Z","service":"thing","message":"the third"}
`[1:])
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
2021-05-03T03:55:50.076Z [thing] the third
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLogsJSON(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/logs")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"n": []string{"30"},
		})
		fmt.Fprintf(w, `
{"time":"2021-05-03T03:55:49.360Z","service":"thing","message":"log 1"}
{"time":"2021-05-03T03:55:49.654Z","service":"snappass","message":"log two"}
{"time":"2021-05-03T03:55:50.076800988Z","service":"thing","message":"the third"}
`[1:])
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "--format", "json"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
{"time":"2021-05-03T03:55:49.36Z","service":"thing","message":"log 1"}
{"time":"2021-05-03T03:55:49.654Z","service":"snappass","message":"log two"}
{"time":"2021-05-03T03:55:50.076800988Z","service":"thing","message":"the third"}
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLogsInvalidFormat(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "--format", "invalid"})
	c.Assert(err.Error(), Equals, `invalid output format (expected "json" or "text", not "invalid")`)
	c.Assert(rest, HasLen, 1)
}

func (s *PebbleSuite) TestLogsN(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/logs")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"n": []string{"2"},
		})
		fmt.Fprintf(w, `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1"}
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two"}
`[1:])
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "-n2"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLogsAll(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/logs")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"n": []string{"-1"},
		})
		fmt.Fprintf(w, `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1"}
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two"}
`[1:])
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "-nall"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestLogsInvalidNumber(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "-ninvalid"})
	c.Assert(err.Error(), Equals, `expected n to be a non-negative integer or "all", not "invalid"`)
	c.Assert(rest, HasLen, 1)
}

func (s *PebbleSuite) TestLogsFollow(c *C) {
	// NOTE: doesn't test actual following behavior -- that's tested in client
	// tests. This just ensures ?follow=true is passed through.
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/logs")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"n":      []string{"30"},
			"follow": []string{"true"},
		})
		fmt.Fprintf(w, `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1"}
`[1:])
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"logs", "-f"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
`[1:])
	c.Check(s.Stderr(), Equals, "")
}
