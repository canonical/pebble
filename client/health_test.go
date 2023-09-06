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

package client_test

import (
	"net/url"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestHealthGet(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": {
			"healthy": true
		}
	}`

	opts := client.HealthOptions{
		Level: client.AliveLevel,
		Names: []string{"chk1", "chk3"},
	}
	health, err := cs.cli.Health(&opts)
	c.Assert(err, check.IsNil)
	c.Assert(health, check.DeepEquals, &client.HealthInfo{
		Healthy: true,
	})
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/health")
	c.Assert(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"level": {"alive"},
		"names": {"chk1", "chk3"},
	})
}
