// Copyright (c) 2024 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestIdentitiesText(c *C) {
	expected := `
Name  Access  Types
bob   read    local
mary  admin   local
`[1:]
	s.testIdentities(c, "", expected)
	s.testIdentities(c, "text", expected)
}

func (s *PebbleSuite) TestIdentitiesYAML(c *C) {
	expected := `
identities:
    bob:
        access: read
        local:
            user-id: 42
    mary:
        access: admin
        local:
            user-id: 1000
`[1:]
	s.testIdentities(c, "yaml", expected)
}

func (s *PebbleSuite) TestIdentitiesJSON(c *C) {
	expected := `{"identities":{"bob":{"access":"read","local":{"user-id":42}},"mary":{"access":"admin","local":{"user-id":1000}}}}` + "\n"
	s.testIdentities(c, "json", expected)
}

func (s *PebbleSuite) testIdentities(c *C, format string, expected string) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/identities")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"bob": {"access": "read", "local": {"user-id": 42}},
				"mary": {"access": "admin", "local": {"user-id": 1000}}
			}
		}`)
	})

	args := []string{"identities"}
	if format != "" {
		args = append(args, "--format", format)
	}
	rest, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, expected)
	c.Check(s.Stderr(), Equals, "")
	s.ResetStdStreams()
}

func (s *PebbleSuite) TestIdentitiesNoIdentities(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v1/identities")
		c.Check(r.URL.Query(), DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identities"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No identities.\n")
}

func (s *PebbleSuite) TestIdentitiesInvalidFormat(c *C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"identities", "--format", "foobar"})
	c.Assert(err, ErrorMatches, "invalid output format.*")
}
