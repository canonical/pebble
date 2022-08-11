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

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestChecks(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "status": "up", "threshold": 3},
		{"name": "chk2", "status": "down", "failures": 1, "threshold": 1, "last-failure": {"error": "small issue"}},
		{"name": "chk3", "level": "alive", "status": "down", "failures": 42, "threshold": 3, "last-failure": {"error": "big problem!"}}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Status  Failures  Last Failure
chk1   -      up      0/3       -
chk2   -      down    1/1       small issue
chk3   alive  down    42/3      big problem!
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChecksFiltering(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{"level": {"alive"}, "names": {"chk1", "chk3"}})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "status": "up", "threshold": 3},
		{"name": "chk3", "level": "alive", "status": "down", "failures": 42, "threshold": 3, "last-failure": {"error": "big problem!"}}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks", "--level=alive", "chk1", "chk3"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Status  Failures  Last Failure
chk1   -      up      0/3       -
chk3   alive  down    42/3      big problem!
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChecksFailures(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "small issue",
        	"details": "with details"
  	    }},
		{"name": "chk2", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "012345678901234567890123456789012345678901234567890123456789truncated"
  	    }},
		{"name": "chk3", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "error",
            "details": "012345678901234567890123456789012345678901234567890123456789truncated"
  	    }}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Status  Failures  Last Failure
chk1   -      down    1/1       small issue: with details
chk2   -      down    1/1       012345678901234567890123456789012345678901234567890123456789...
chk3   -      down    1/1       error: 01234567890123456789012345678901234567890123456789012...
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChecksFailuresVerbose(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/checks")
		c.Assert(r.URL.Query(), check.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": [
		{"name": "chk1", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "small issue",
        	"details": "with details"
  	    }},
		{"name": "chk2", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "012345678901234567890123456789012345678901234567890123456789 not truncated"
  	    }},
		{"name": "chk3", "status": "down", "failures": 1, "threshold": 1, "last-failure": {
            "error": "error",
            "details": "  The quick   \r\nbrown fox\n\njumps over\nthe lazy dog.    \n\n\n"
  	    }}
	]
}`)
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"checks", "--verbose"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Check  Level  Status  Failures  Last Failure
chk1   -      down    1/1       small issue
                                with details
chk2   -      down    1/1       012345678901234567890123456789012345678901234567890123456789 not truncated
chk3   -      down    1/1       error
                                  The quick
                                brown fox
                                
                                jumps over
                                the lazy dog.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}
