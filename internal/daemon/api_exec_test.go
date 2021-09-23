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
	stdout, stderr, exitCode, waitErr := s.exec(c, "foo bar", &client.ExecOptions{
		Command: []string{"cat"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "foo bar")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestStderr(c *C) {
	stdout, stderr, exitCode, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo some stderr! >&2"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "some stderr!\n")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestCombinedStderr(c *C) {
	outBuf := &bytes.Buffer{}
	opts := &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR! >&2"},
		Stdout:  outBuf,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)
	exitCode, waitErr := process.Wait()
	c.Check(err, IsNil)
	c.Check(waitErr, IsNil)
	c.Check(outBuf.String(), Equals, "OUT\nERR!\n")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestEnvironment(c *C) {
	stdout, stderr, exitCode, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment: map[string]string{"FOO": "bar"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "FOO=bar\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestWorkingDir(c *C) {
	workingDir := c.MkDir()
	stdout, stderr, exitCode, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: workingDir,
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, workingDir+"\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) TestTimeout(c *C) {
	stdout, stderr, _, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Timeout: 10 * time.Millisecond,
	})
	c.Check(waitErr, ErrorMatches, `cannot perform the following tasks:\n.*timed out after 10ms.*`)
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "")
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
	stdout, stderr, exitCode, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		User:    "nobody",
		Group:   "nogroup",
	})
	c.Check(waitErr, IsNil)
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
	stdout, stderr, exitCode, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		UserID:  &uid,
		GroupID: &gid,
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "nobody\nnogroup\n")
	c.Check(stderr, Equals, "")
	c.Check(exitCode, Equals, 0)
}

func (s *execSuite) exec(c *C, stdin string, opts *client.ExecOptions) (stdout, stderr string, exitCode int, waitErr error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	opts.Stdin = strings.NewReader(stdin)
	opts.Stdout = outBuf
	opts.Stderr = errBuf
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	exitCode, waitErr = process.Wait()
	c.Check(err, IsNil)

	return outBuf.String(), errBuf.String(), exitCode, waitErr
}

func (s *execSuite) TestSignal(c *C) {
	opts := &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Stdin:   strings.NewReader(""),
		Stdout:  ioutil.Discard,
		Stderr:  ioutil.Discard,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	err = process.SendSignal("SIGINT")
	c.Assert(err, IsNil)

	exitCode, err := process.Wait()
	c.Check(err, IsNil)

	c.Check(exitCode, Equals, 130)
}

func (s *execSuite) TestStreaming(c *C) {
	stdinCh := make(chan []byte)
	stdoutCh := make(chan []byte)
	opts := &client.ExecOptions{
		Command: []string{"cat"},
		Stdin:   channelReader{stdinCh},
		Stdout:  channelWriter{stdoutCh},
		Stderr:  ioutil.Discard,
	}
	process, err := s.client.Exec(opts)
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

	exitCode, err := process.Wait()
	c.Check(err, IsNil)
	c.Check(exitCode, Equals, 0)
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

func (s *execSuite) TestCommandNotFound(c *C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"badcmd"},
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

// execRequest directly calls exec via the ServeHTTP endpoint, rather than
// using the Go client.
func execRequest(c *C, opts *client.ExecOptions) (*http.Response, execResponse) {
	var timeoutStr string
	if opts.Timeout != 0 {
		timeoutStr = opts.Timeout.String()
	}
	payload := execPayload{
		Command:     opts.Command,
		Environment: opts.Environment,
		WorkingDir:  opts.WorkingDir,
		Timeout:     timeoutStr,
		UserID:      opts.UserID,
		User:        opts.User,
		GroupID:     opts.GroupID,
		Group:       opts.Group,
		UseTerminal: opts.UseTerminal,
		SplitStderr: opts.Stderr != nil,
		Width:       opts.Width,
		Height:      opts.Height,
	}
	requestBody, err := json.Marshal(&payload)
	c.Assert(err, IsNil)

	httpResp, body := doRequest(c, v1PostExec, "POST", "/v1/exec", nil, nil, requestBody)
	var execResp execResponse
	err = json.Unmarshal(body.Bytes(), &execResp)
	c.Assert(err, IsNil)
	return httpResp, execResp
}
