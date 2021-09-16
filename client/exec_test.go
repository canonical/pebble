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

package client

import (
	"bytes"
	"encoding/json"
	"io"

	. "gopkg.in/check.v1"
)

type execSuite struct{}

var _ = Suite(&execSuite{})

func (s *execSuite) TestSendSignal(c *C) {
	buf := &bytes.Buffer{}
	execution := &Execution{
		controlConn: testJSONWriter{buf},
	}

	err := execution.SendSignal("SIGHUP")
	c.Check(err, IsNil)
	err = execution.SendSignal("SIGUSR1")
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"signal","signal":{"name":"SIGHUP"}}
{"command":"signal","signal":{"name":"SIGUSR1"}}
`[1:])
}

func (s *execSuite) TestSendResize(c *C) {
	buf := &bytes.Buffer{}
	execution := &Execution{
		controlConn: testJSONWriter{buf},
	}

	err := execution.SendResize(150, 50)
	c.Check(err, IsNil)
	err = execution.SendResize(80, 25)
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"resize","resize":{"width":150,"height":50}}
{"command":"resize","resize":{"width":80,"height":25}}
`[1:])
}

type testJSONWriter struct {
	w io.Writer
}

func (w testJSONWriter) WriteJSON(v interface{}) error {
	encoder := json.NewEncoder(w.w)
	return encoder.Encode(v)
}
