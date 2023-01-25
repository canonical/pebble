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
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestShutdown(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK"
	}`
	err := cs.cli.Shutdown(&client.ShutdownOptions{})
	c.Assert(err, IsNil)
	c.Assert(cs.req.URL.Path, Equals, "/v1/shutdown")
	c.Assert(cs.req.Method, Equals, "POST")
}
