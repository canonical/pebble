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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
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

func (s *execSuite) TestStdinStdout(c *C) {
	changeErr, stdout, stderr, exitCode := s.exec(c, "foo bar", &client.ExecOptions{
		Command:        []string{"cat"},
		SeparateStderr: true,
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, "foo bar")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestStderr(c *C) {
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo some stderr! >&2"},
		SeparateStderr: true,
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "some stderr!\n")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestEnvironment(c *C) {
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment:    map[string]string{"FOO": "bar"},
		SeparateStderr: true,
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, "FOO=bar\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestWorkingDir(c *C) {
	workingDir := c.MkDir()
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"pwd"},
		SeparateStderr: true,
		WorkingDir:     workingDir,
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, workingDir+"\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestTimeout(c *C) {
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"sleep", "1"},
		SeparateStderr: true,
		Timeout:        time.Millisecond,
	})
	c.Check(changeErr, Matches, `cannot perform the following tasks:\n.*timed out after 1ms.*`)
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, -1)
}

// You can run these tests as root with the following commands:
//
// go test -c -v ./internal/daemon
// sudo ./daemon.test -check.v -check.f execSuite
//
func (s *execSuite) TestUserGroup(c *C) {
	if os.Getuid() != 0 {
		c.Skip("exec user/group test requires running as root")
	}
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		SeparateStderr: true,
		User:           "nobody",
		Group:          "nogroup",
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, "nobody\nnogroup\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestUserIDGroupID(c *C) {
	if os.Getuid() != 0 {
		c.Skip("exec user ID/group ID test requires running as root")
	}
	nobody, err := user.Lookup("nobody")
	c.Assert(err, IsNil)
	nogroup, err := user.LookupGroup("nogroup")
	c.Assert(err, IsNil)
	uid, err := strconv.Atoi(nobody.Uid)
	c.Assert(err, IsNil)
	gid, err := strconv.Atoi(nogroup.Gid)
	c.Assert(err, IsNil)
	changeErr, stdout, stderr, exitCode := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		SeparateStderr: true,
		UserID:         &uid,
		GroupID:        &gid,
	})
	c.Check(changeErr, Equals, "")
	c.Check(stdout, Equals, "nobody\nnogroup\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) exec(c *C, stdin string, opts *client.ExecOptions) (changeErr, stdout, stderr string, exitCode int) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	opts.Stdin = ioutil.NopCloser(strings.NewReader(stdin))
	opts.Stdout = outBuf
	opts.Stderr = errBuf
	opts.DataDone = make(chan bool)
	changeID, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	change, err := s.client.WaitChange(changeID, nil)
	c.Check(err, IsNil)
	c.Check(change.Ready, Equals, true)
	exitCode = getExitCode(c, change)

	<-opts.DataDone

	return change.Err, outBuf.String(), errBuf.String(), exitCode
}

func (s *execSuite) TestSignal(c *C) {
	signalCh := make(chan int, 1)
	opts := &client.ExecOptions{
		Command:        []string{"sleep", "1"},
		SeparateStderr: true,
		Stdin:          ioutil.NopCloser(strings.NewReader("")),
		Stdout:         ioutil.Discard,
		Stderr:         ioutil.Discard,
		Control: func(conn client.WebsocketWriter) {
			signal := <-signalCh
			err := client.ExecForwardSignal(conn, signal)
			c.Check(err, IsNil)
		},
	}
	changeID, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	select {
	case signalCh <- int(unix.SIGINT):
	case <-time.After(time.Second):
		c.Fatalf("timed out sending to signal channel")
	}

	change, err := s.client.WaitChange(changeID, nil)
	c.Check(err, IsNil)
	c.Check(change.Ready, Equals, true)
	c.Check(change.Err, Equals, "")

	c.Check(getExitCode(c, change), Equals, 130)
}

func (s *execSuite) TestStreaming(c *C) {
	stdinCh := make(chan []byte)
	stdoutCh := make(chan []byte)
	opts := &client.ExecOptions{
		Command:        []string{"cat"},
		SeparateStderr: true,
		Stdin:          ioutil.NopCloser(channelReader{stdinCh}),
		Stdout:         channelWriter{stdoutCh},
		Stderr:         ioutil.Discard,
	}
	changeID, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	for i := 0; i < 20; i++ {
		chunk := fmt.Sprintf("chunk %d ", i)
		select {
		case stdinCh <- []byte(chunk):
		case <-time.After(time.Second):
			c.Fatalf("timed out waiting to write to stdin")
		}
		select {
		case b := <-stdoutCh:
			c.Check(string(b), Equals, chunk)
		case <-time.After(time.Second):
			c.Fatalf("timed out waiting for stdout")
		}
	}

	select {
	case stdinCh <- nil:
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting to write to stdin")
	}

	change, err := s.client.WaitChange(changeID, nil)
	c.Check(err, IsNil)
	c.Check(change.Ready, Equals, true)
	c.Check(change.Err, Equals, "")
	c.Check(getExitCode(c, change), Equals, 0)
}

type channelReader struct {
	ch chan []byte
}

func (r channelReader) Read(buf []byte) (int, error) {
	b := <-r.ch
	if b == nil {
		return 0, io.EOF
	}
	n := copy(buf, b)
	return n, nil
}

type channelWriter struct {
	ch chan []byte
}

func (w channelWriter) Write(buf []byte) (int, error) {
	w.ch <- buf
	return len(buf), nil
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
		Command:        []string{"echo", "foo"},
		UseTerminal:    true,
		SeparateStderr: true,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*not currently supported.*")

	httpResp, execResp = execRequest(c, &client.ExecOptions{
		Command:        []string{"echo", "foo"},
		UseTerminal:    false,
		SeparateStderr: false,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*not currently supported.*")
}

func (s *execSuite) TestCommandNotFound(c *C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command:        []string{"badcmd"},
		SeparateStderr: true,
	})
	c.Check(httpResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, Equals, http.StatusBadRequest)
	c.Check(execResp.Type, Equals, "error")
	c.Check(execResp.Result["message"], Matches, ".*executable file not found.*")
}

func (s *execSuite) TestUserGroupError(c *C) {
	gid := os.Getgid()
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command:        []string{"echo", "foo"},
		SeparateStderr: true,
		GroupID:        &gid,
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

// execRequest directly calls exec via the ServeHTTP endpoint, rather than
// going through the Go client.
func execRequest(c *C, opts *client.ExecOptions) (*http.Response, execResponse) {
	var timeoutStr string
	if opts.Timeout != 0 {
		timeoutStr = opts.Timeout.String()
	}
	var payload = struct {
		Command        []string          `json:"command"`
		Environment    map[string]string `json:"environment"`
		WorkingDir     string            `json:"working-dir"`
		Timeout        string            `json:"timeout"`
		UserID         *int              `json:"user-id"`
		User           string            `json:"user"`
		GroupID        *int              `json:"group-id"`
		Group          string            `json:"group"`
		Terminal       bool              `json:"use-terminal"`
		SeparateStderr bool              `json:"separate-stderr"`
		Width          int               `json:"width"`
		Height         int               `json:"height"`
	}{
		Command:        opts.Command,
		Environment:    opts.Environment,
		WorkingDir:     opts.WorkingDir,
		Timeout:        timeoutStr,
		UserID:         opts.UserID,
		User:           opts.User,
		GroupID:        opts.GroupID,
		Group:          opts.Group,
		Terminal:       opts.UseTerminal,
		SeparateStderr: opts.SeparateStderr,
		Width:          opts.Width,
		Height:         opts.Height,
	}
	requestBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)

	httpResp, body := doRequest(c, v1PostExec, "POST", "/v1/exec", nil, nil, requestBody)
	var execResp execResponse
	err = json.Unmarshal(body.Bytes(), &execResp)
	c.Assert(err, IsNil)
	return httpResp, execResp
}

func getExitCode(c *C, change *client.Change) int {
	exitCode := 1
	_ = change.Get("exit-code", &exitCode)
	return exitCode
}
