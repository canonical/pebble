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

package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

var _ = Suite(&execSuite{})

type execSuite struct {
	daemon *Daemon
	client *client.Client
}

func (s *execSuite) SetUpTest(c *C) {
	socketPath := c.MkDir() + ".pebble.socket"
	daemon, err := New(&Options{
		Dir:        c.MkDir(),
		SocketPath: socketPath,
	})
	c.Assert(err, IsNil)
	err = daemon.Init()
	c.Assert(err, IsNil)
	daemon.Start()
	s.daemon = daemon

	s.client = client.New(&client.Config{Socket: socketPath})
}

func (s *execSuite) TearDownTest(c *C) {
	err := s.daemon.Stop(nil)
	c.Check(err, IsNil)
}

// Some of these tests use the Go client (tested elsewhere) for simplicity.

func (s *execSuite) TestSimple(c *C) {
	stdout, stderr, exitCode := s.execSimple(c, "", &client.ExecOptions{
		Command: []string{"echo", "foo", "bar"},
		Stderr:  true,
	})
	c.Check(stdout, Equals, "foo bar\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) execSimple(c *C, stdin string, opts *client.ExecOptions) (stdout, stderr string, exitCode int) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	args := &client.ExecAdditionalArgs{
		Stdin:    io.NopCloser(strings.NewReader(stdin)),
		Stdout:   writerNopCloser{outBuf},
		Stderr:   writerNopCloser{errBuf},
		Control:  func(conn *websocket.Conn) {},
		DataDone: make(chan bool),
	}
	changeID, err := s.client.Exec(opts, args)
	c.Check(err, IsNil)

	change, err := s.client.WaitChange(changeID, nil)
	c.Check(change.Ready, Equals, true)
	c.Check(change.Err, Equals, "")
	err = change.Get("return", &exitCode)
	if err != nil {
		exitCode = -1
	}

	<-args.DataDone

	return outBuf.String(), errBuf.String(), exitCode
}

type writerNopCloser struct {
	io.Writer
}

func (c writerNopCloser) Close() error {
	return nil
}

func (s *execSuite) TestNoCommand(c *C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Equals, "must specify command")
}

func (s *execSuite) TestNotSupported(c *C) {
	// These combinations aren't currently supported (but will be later)
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command:  []string{"echo", "foo"},
		Terminal: true,
		Stderr:   true,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*not currently supported.*")

	httpResp, execResp = execRequest(c, &client.ExecOptions{
		Command:  []string{"echo", "foo"},
		Terminal: false,
		Stderr:   false,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*not currently supported.*")
}

func (s *execSuite) TestCommandNotFound(c *C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"badcmd"},
		Stderr:  true,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*executable file not found.*")
}

func (s *execSuite) TestUserGroupError(c *C) {
	gid := os.Getgid()
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"echo", "foo"},
		Stderr:  true,
		GroupID: &gid,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*must specify user, not just group.*")
}

type execResponse struct {
	StatusCode int                    `json:"status-code"`
	Type       string                 `json:"type"`
	Change     string                 `json:"change"`
	Result     map[string]interface{} `json:"result"`
}

func execRequest(c *C, options *client.ExecOptions) (*http.Response, execResponse) {
	var payload = struct {
		Command     []string          `json:"command"`
		Environment map[string]string `json:"environment"`
		WorkingDir  string            `json:"working-dir"`
		Timeout     time.Duration     `json:"timeout"`
		UserID      *int              `json:"user-id"`
		User        string            `json:"user"`
		GroupID     *int              `json:"group-id"`
		Group       string            `json:"group"`
		Terminal    bool              `json:"terminal"`
		Stderr      bool              `json:"stderr"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
	}(*options)
	requestBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)

	httpResp, body := doRequest(c, v1PostExec, "POST", "/v1/exec", nil, nil, requestBody)
	var execResp execResponse
	err = json.Unmarshal(body.Bytes(), &execResp)
	c.Assert(err, IsNil)
	return httpResp, execResp
}
