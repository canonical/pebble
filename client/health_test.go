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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestHealthGet(c *tc.C) {
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
	c.Assert(err, tc.IsNil)
	c.Assert(health, tc.Equals, true)
	c.Assert(cs.req.Method, tc.Equals, "GET")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/health")
	c.Assert(cs.req.URL.Query(), tc.DeepEquals, url.Values{
		"level": {"alive"},
		"names": {"chk1", "chk3"},
	})
}

func (cs *clientSuite) TestHealthDefaultOptions(c *tc.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 502,
		"status": "Bad Gateway",
		"result": {
			"healthy": false
		}
	}`

	health, err := cs.cli.Health(&client.HealthOptions{})
	c.Assert(err, tc.IsNil)
	c.Assert(health, tc.Equals, false)
	c.Assert(cs.req.Method, tc.Equals, "GET")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/health")
	c.Assert(cs.req.URL.Query(), tc.DeepEquals, url.Values{})
}
