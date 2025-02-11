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

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestAddLayer(c *check.C) {
	for _, option := range []struct {
		combine bool
		inner   bool
	}{{
		combine: false,
		inner:   false,
	}, {
		combine: true,
		inner:   false,
	}, {
		combine: false,
		inner:   true,
	}, {
		combine: true,
		inner:   true,
	}} {
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
		err := cs.cli.AddLayer(&client.AddLayerOptions{
			Combine:   option.combine,
			Inner:     option.inner,
			Label:     "foo",
			LayerData: []byte(layerYAML),
		})
		c.Assert(err, check.IsNil)
		c.Check(cs.req.Method, check.Equals, "POST")
		c.Check(cs.req.URL.Path, check.Equals, "/v1/layers")
		c.Check(cs.req.URL.Query(), check.HasLen, 0)
		var body map[string]any
		c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
		c.Assert(body, check.DeepEquals, map[string]any{
			"action":  "add",
			"combine": option.combine,
			"label":   "foo",
			"format":  "yaml",
			"layer":   layerYAML,
			"inner":   option.inner,
		})
	}
}

func (cs *clientSuite) TestPlanBytes(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": "services:\n    foo:\n        override: replace\n        command: cmd\n"
	}`
	data, err := cs.cli.PlanBytes(&client.PlanOptions{})
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
