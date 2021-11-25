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
	"net/url"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestChecksGet(c *check.C) {
	cs.rsp = `{
		"result": [
			{"name": "chk1", "healthy": true},
			{"name": "chk3", "healthy": false, "failures": 42, "last-error": "big error", "error-details": "details..."}
		],
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	opts := client.ChecksOptions{
		Level: client.AliveLevel,
		Names: []string{"chk1", "chk3"},
	}
	checks, err := cs.cli.Checks(&opts)
	c.Assert(err, check.IsNil)
	c.Assert(checks, check.DeepEquals, []*client.CheckInfo{
		{
			Name:    "chk1",
			Healthy: true,
		},
		{
			Name:         "chk3",
			Healthy:      false,
			Failures:     42,
			LastError:    "big error",
			ErrorDetails: "details...",
		},
	})
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/checks")
	c.Assert(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"level": {"alive"},
		"names": {"chk1", "chk3"},
	})
}
