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
	w := testWebsocketWriter{buf}

	err := client.ExecSendSignal(w, "SIGHUP")
	c.Check(err, IsNil)
	err = client.ExecSendSignal(w, "SIGUSR1")
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"signal","signal":{"name":"SIGHUP"}}
{"command":"signal","signal":{"name":"SIGUSR1"}}
`[1:])
}

func (s *execSuite) TestSendTermSize(c *C) {
	buf := &bytes.Buffer{}
	w := testWebsocketWriter{buf}

	err := client.ExecSendResize(w, 150, 50)
	c.Check(err, IsNil)
	err = client.ExecSendResize(w, 80, 25)
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"resize","resize":{"width":150,"height":50}}
{"command":"resize","resize":{"width":80,"height":25}}
`[1:])
}

type testWebsocketWriter struct {
	w io.Writer
}

func (w testWebsocketWriter) WriteMessage(messageType int, data []byte) error {
	panic("not implemented")
}

func (w testWebsocketWriter) WriteJSON(v interface{}) error {
	encoder := json.NewEncoder(w.w)
	return encoder.Encode(v)
}
