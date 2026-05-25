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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestSignals(c *tc.C) {
	cs.rsp = `{
		"result": true,
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`
	err := cs.cli.SendSignal(&client.SendSignalOptions{
		Signal:   "SIGHUP",
		Services: []string{"s1", "s2"},
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.req.Method, tc.Equals, "POST")
	c.Check(cs.req.URL.Path, tc.Equals, "/v1/signals")

	var body map[string]any
	err = json.NewDecoder(cs.req.Body).Decode(&body)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(body, tc.DeepEquals, map[string]any{
		"signal":   "SIGHUP",
		"services": []any{"s1", "s2"},
	})
}
