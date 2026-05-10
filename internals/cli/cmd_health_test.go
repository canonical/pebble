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

func (s *PebbleSuite) TestHealth(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/health")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprintf(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {"healthy": true}
		}`)
	})

	restore := fakeArgs("pebble", "health")
	defer restore()

	exitCode := cli.PebbleMain()
	c.Check(exitCode, tc.Equals, 0)
	c.Check(s.Stdout(), tc.Equals, "healthy\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestHealthLevel(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/health")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{"level": {"alive"}})
		fmt.Fprintf(w, `{
			"type": "sync",
			"status-code": 502,
			"result": {"healthy": false}
		}`)
	})

	restore := fakeArgs("pebble", "health", "--level", "alive")
	defer restore()

	exitCode := cli.PebbleMain()
	c.Check(exitCode, tc.Equals, 1)
	c.Check(s.Stdout(), tc.Equals, "unhealthy\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestHealthSpecificChecks(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/health")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{
			"level": {"ready"},
			"names": {"chk1", "chk3"},
		})
		fmt.Fprintf(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {"healthy": true}
		}`)
	})

	restore := fakeArgs("pebble", "health", "--level", "ready", "chk1", "chk3")
	defer restore()

	exitCode := cli.PebbleMain()
	c.Check(exitCode, tc.Equals, 0)
	c.Check(s.Stdout(), tc.Equals, "healthy\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestHealthBadLevel(c *tc.C) {
	restore := fakeArgs("pebble", "health", "--level", "foo")
	defer restore()

	exitCode := cli.PebbleMain()
	c.Check(exitCode, tc.Equals, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Matches, "error: Invalid value .* Allowed values are: alive or ready\n")
}
