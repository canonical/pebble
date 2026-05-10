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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestIdentitiesText(c *tc.C) {
	expected := `
Name  Access  Types
bob   read    local
mary  admin   local
`[1:]
	s.testIdentities(c, "", expected)
	s.testIdentities(c, "text", expected)
}

func (s *PebbleSuite) TestIdentitiesYAML(c *tc.C) {
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

func (s *PebbleSuite) TestIdentitiesJSON(c *tc.C) {
	expected := `{"identities":{"bob":{"access":"read","local":{"user-id":42}},"mary":{"access":"admin","local":{"user-id":1000}}}}` + "\n"
	s.testIdentities(c, "json", expected)
}

func (s *PebbleSuite) testIdentities(c *tc.C, format string, expected string) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
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
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, expected)
	c.Check(s.Stderr(), tc.Equals, "")
	s.ResetStdStreams()
}

func (s *PebbleSuite) TestIdentitiesNoIdentities(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identities"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "No identities.\n")
}

func (s *PebbleSuite) TestIdentitiesNoIdentitiesJSON(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identities", "--format", "json"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"identities":{}}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestIdentitiesNoIdentitiesYAML(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/identities")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{})
		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"identities", "--format", "yaml"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "identities: {}\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestIdentitiesInvalidFormat(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"identities", "--format", "foobar"})
	c.Assert(err, tc.ErrorMatches, "Invalid value.*for option.*--format.*")
}
