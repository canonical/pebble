// Copyright (c) 2014-2020 Canonical Ltd
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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestStartStop(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{
		Names: []string{"one", "two"},
	}

	for i := 0; i < 2; i++ {
		cs.req = nil

		startStop := cs.cli.Start
		action := "start"
		if i == 1 {
			startStop = cs.cli.Stop
			action = "stop"
		}

		changeId, err := startStop(&opts)
		c.Check(err, check.IsNil)
		c.Check(changeId, check.Equals, "42")
		c.Check(cs.req.Method, check.Equals, "POST")
		c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

		var body map[string]interface{}
		c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
		c.Check(body, check.HasLen, 2)
		c.Check(body["action"], check.Equals, action)
		c.Check(body["services"], check.DeepEquals, []interface{}{"one", "two"})
	}
}

func (cs *clientSuite) TestAutoStart(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{}

	changeId, err := cs.cli.AutoStart(&opts)
	c.Check(err, check.IsNil)
	c.Check(changeId, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

	var body map[string]interface{}
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "autostart")
}
