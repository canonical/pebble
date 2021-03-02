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

package client_test

import (
	"encoding/json"
	"net/url"

	"github.com/canonical/pebble/client"
	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestAddLayer(c *check.C) {
	for _, action := range []string{"", "combine"} {
		cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": true
	}`
		data := `
services:
 foo:
  override: replace
  command: cmd
`[1:]
		err := cs.cli.AddLayer(&client.AddLayerOptions{
			Action:    client.AddLayerAction(action),
			LayerData: []byte(data),
		})
		c.Assert(err, check.IsNil)
		c.Check(cs.req.Method, check.Equals, "POST")
		c.Check(cs.req.URL.Path, check.Equals, "/v1/layers")
		c.Check(cs.req.URL.Query(), check.HasLen, 0)
		var body map[string]interface{}
		c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
		c.Assert(body, check.DeepEquals, map[string]interface{}{
			"action": "combine",
			"format": "yaml",
			"layer":  data,
		})
	}
}

func (cs *clientSuite) TestPlanData(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": "services:\n foo:\n  override: replace\n  command: cmd\n"
	}`
	data, err := cs.cli.PlanData(&client.PlanDataOptions{})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/plan")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{"format": []string{"yaml"}})
	c.Assert(string(data), check.Equals, `
services:
 foo:
  override: replace
  command: cmd
`[1:])
}
