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

	"github.com/canonical/pebble/client"
	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestMergeLayer(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": true
	}`
	layerYAML := `
services:
 foo:
  override: replace
  command: cmd
`[1:]
	err := cs.cli.MergeLayer(&client.MergeLayerOptions{Layer: layerYAML})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/layers")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)
	var body map[string]interface{}
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Assert(body, check.DeepEquals, map[string]interface{}{
		"action": "merge",
		"format": "yaml",
		"layer":  layerYAML,
	})
}

func (cs *clientSuite) TestFlattenedSetup(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": "services:\n foo:\n  override: replace\n  command: cmd\n"
	}`
	layerYAML, err := cs.cli.FlattenedSetup(&client.FlattenedSetupOptions{})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/layers")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)
	var body map[string]interface{}
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Assert(body, check.DeepEquals, map[string]interface{}{
		"action": "flatten",
		"format": "yaml",
	})
	c.Assert(layerYAML, check.Equals, `
services:
 foo:
  override: replace
  command: cmd
`[1:])
}
