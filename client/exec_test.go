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
	"bytes"
	"encoding/json"
	"io"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

type execSuite struct{}

var _ = Suite(&execSuite{})

func (s *execSuite) TestForwardSignal(c *C) {
	buf := &bytes.Buffer{}
	w := testWebsocketConn{w: buf}

	err := client.ExecForwardSignal(w, 1)
	c.Check(err, IsNil)
	err = client.ExecForwardSignal(w, 42)
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"signal","signal":1}
{"command":"signal","signal":42}
`[1:])
}

func (s *execSuite) TestSendTermSize(c *C) {
	buf := &bytes.Buffer{}
	w := testWebsocketConn{w: buf}

	err := client.ExecSendTermSize(w, 150, 50)
	c.Check(err, IsNil)
	err = client.ExecSendTermSize(w, 80, 25)
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"window-resize","args":{"height":"50","width":"150"}}
{"command":"window-resize","args":{"height":"25","width":"80"}}
`[1:])
}

type testWebsocketConn struct {
	w io.Writer
	client.WebsocketConn
}

func (w testWebsocketConn) WriteJSON(v interface{}) error {
	encoder := json.NewEncoder(w.w)
	return encoder.Encode(v)
}
