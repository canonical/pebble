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

package client_test

import (
	"encoding/json"
	"net/url"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestChecksGet(c *check.C) {
	cs.rsp = `{
		"result": [
			{"name": "chk1", "status": "up"},
			{"name": "chk3", "status": "down", "failures": 42},
			{"name": "chk5", "status": "inactive"}
		],
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	opts := client.ChecksOptions{
		Level: client.AliveLevel,
		Names: []string{"chk1", "chk3", "chk5"},
	}
	checks, err := cs.cli.Checks(&opts)
	c.Assert(err, check.IsNil)
	c.Assert(checks, check.DeepEquals,
		[]*client.CheckInfo{{
			Name:   "chk1",
			Status: client.CheckStatusUp,
		}, {
			Name:     "chk3",
			Status:   client.CheckStatusDown,
			Failures: 42,
		}, {
			Name:   "chk5",
			Status: client.CheckStatusInactive,
		}})
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/checks")
	c.Assert(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"level": {"alive"},
		"names": {"chk1", "chk3", "chk5"},
	})
}

func (cs *clientSuite) TestStartChecks(c *check.C) {
	cs.rsp = `{
		"result": {"changed": ["chk1", "chk2"]},
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	opts := client.ChecksActionOptions{
		Names: []string{"chk1", "chk2"},
	}
	results, err := cs.cli.StartChecks(&opts)
	c.Check(err, check.IsNil)
	c.Check(results.Changed, check.DeepEquals, []string{"chk1", "chk2"})
	c.Assert(cs.req.Method, check.Equals, "POST")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/checks")

	var body map[string]any
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "start")
	c.Check(body["checks"], check.DeepEquals, []any{"chk1", "chk2"})

}

func (cs *clientSuite) TestStopChecks(c *check.C) {
	cs.rsp = `{
		"result": {"changed": ["chk1"]},
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	opts := client.ChecksActionOptions{
		Names: []string{"chk1", "chk2"},
	}
	results, err := cs.cli.StopChecks(&opts)
	c.Check(err, check.IsNil)
	c.Check(results.Changed, check.DeepEquals, []string{"chk1"})
	c.Assert(cs.req.Method, check.Equals, "POST")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/checks")

	var body map[string]any
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "stop")
	c.Check(body["checks"], check.DeepEquals, []any{"chk1", "chk2"})
}
