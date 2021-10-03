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
	"fmt"
	"io"
	"io/ioutil"

	"github.com/gorilla/websocket"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

type execSuite struct {
	clientSuite
	ws *testWebsocket
}

var _ = Suite(&execSuite{})

func (s *execSuite) SetUpTest(c *C) {
	s.clientSuite.SetUpTest(c)

	s.ws = &testWebsocket{}
	s.cli.SetGetWebsocket(func(url string) (client.ClientWebsocket, error) {
		return s.ws, nil
	})
}

func (s *execSuite) TearDownTest(c *C) {
	s.clientSuite.TearDownTest(c)
}

type testWebsocket struct {
	reads  []read
	writes []write
}

type read struct {
	messageType int
	data        string
}

type write struct {
	messageType int
	data        string
}

func (w *testWebsocket) NextReader() (messageType int, r io.Reader, err error) {
	if len(w.reads) == 0 {
		return websocket.CloseMessage, nil, nil
	}
	read := w.reads[0]
	w.reads = w.reads[1:]
	return read.messageType, bytes.NewBufferString(read.data), nil
}

func (w *testWebsocket) WriteMessage(messageType int, data []byte) error {
	w.writes = append(w.writes, write{messageType, string(data)})
	return nil
}

func (w *testWebsocket) Close() error {
	return nil
}

func (w *testWebsocket) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.writes = append(w.writes, write{websocket.TextMessage, string(data)})
	return nil
}

func (s *execSuite) addResponses(changeID string, exitCode int) {
	// Add /v1/exec response
	taskID := "T" + changeID
	s.rsps = append(s.rsps, fmt.Sprintf(`{
		"change": "%s",
		"result": {"task-id": "%s"},
		"status": "Accepted",
		"status-code": 202,
		"type": "async"
	}`, changeID, taskID))

	// Add /v1/changes/{id}/wait response
	s.rsps = append(s.rsps, fmt.Sprintf(`{
		"result": {
			"id": "%s",
			"kind": "exec",
			"ready": true,
			"tasks": [{
				"data": {"exit-code": %d},
				"id": "%s",
				"kind": "exec"
			}]
		},
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`, changeID, exitCode, taskID))
}

func (s *execSuite) TestExitZero(c *C) {
	s.addResponses("123", 0)
	opts := &client.ExecOptions{
		Command: []string{"true"},
	}

	process, err := s.cli.Exec(opts)
	c.Assert(err, IsNil)
	c.Assert(s.req.Method, Equals, "POST")
	c.Assert(s.req.URL.String(), Equals, "http://localhost/v1/exec")
	body, err := ioutil.ReadAll(s.req.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, `{"command":["true"]}`+"\n")

	err = process.Wait()
	c.Assert(err, IsNil)
	c.Assert(s.req.Method, Equals, "GET")
	c.Assert(s.req.URL.String(), Equals, "http://localhost/v1/changes/123/wait")
}

type execControlSuite struct{}

var _ = Suite(&execControlSuite{})

func (s *execControlSuite) TestSendSignal(c *C) {
	buf := &bytes.Buffer{}
	process := &client.ExecProcess{}
	process.SetControlConn(testJSONWriter{buf})

	err := process.SendSignal("SIGHUP")
	c.Check(err, IsNil)
	err = process.SendSignal("SIGUSR1")
	c.Check(err, IsNil)

	c.Check(buf.String(), Equals, `
{"command":"signal","signal":{"name":"SIGHUP"}}
{"command":"signal","signal":{"name":"SIGUSR1"}}
`[1:])
}

func (s *execControlSuite) TestSendResize(c *C) {
	buf := &bytes.Buffer{}
	process := &client.ExecProcess{}
	process.SetControlConn(testJSONWriter{buf})

	err := process.SendResize(150, 50)
	c.Check(err, IsNil)
	err = process.SendResize(80, 25)
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
