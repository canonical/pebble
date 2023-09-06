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
	"fmt"
	"io"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestNotifyBasic(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/notices")

		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var m map[string]any
		err = json.Unmarshal(body, &m)
		c.Assert(err, IsNil)
		c.Check(m, DeepEquals, map[string]any{
			"action": "add",
			"type":   "client",
			"key":    "a.b/c",
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {"id": "123"}
		}`)
	})

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"notify", "a.b/c"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "Recorded notice 123\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestNotifyData(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v1/notices")

		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var m map[string]any
		err = json.Unmarshal(body, &m)
		c.Assert(err, IsNil)
		c.Check(m, DeepEquals, map[string]any{
			"action": "add",
			"type":   "client",
			"key":    "a.b/c",
			"data": map[string]any{
				"k":   "v",
				"foo": "bar bazz",
			},
			"repeat-after": "1h0m0s",
		})

		fmt.Fprint(w, `{
			"type": "sync",
			"status-code": 200,
			"result": {"id": "42"}
		}`)
	})

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{
		"notify", "--repeat-after=1h", "a.b/c", "k=v", "foo=bar bazz"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "Recorded notice 42\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestNotifyInvalidData(c *C) {
	_, err := cli.Parser(cli.Client()).ParseArgs([]string{"notify", "a.b/c", "bad"})
	c.Assert(err, ErrorMatches, "data args.*")
}
