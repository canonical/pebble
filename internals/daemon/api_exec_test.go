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
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
)

var _ = Suite(&execSuite{})

type execSuite struct {
	daemon *Daemon
	client *client.Client
}

func (s *execSuite) SetUpSuite(c *C) {
	logger.SetLogger(logger.New(os.Stderr, "[test] "))
}

func (s *execSuite) SetUpTest(c *C) {
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

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

	s.client, err = client.New(&client.Config{Socket: socketPath})
	c.Assert(err, IsNil)
}

func (s *execSuite) TearDownTest(c *C) {
	err := s.daemon.Stop(nil)
	c.Check(err, IsNil)

	err = reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
}

// Some of these tests use the Go client for simplicity.

func (s *execSuite) TestStdinStdout(c *C) {
	stdout, stderr, waitErr := s.exec(c, "foo bar", &client.ExecOptions{
		Command: []string{"cat"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "foo bar")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestStderr(c *C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo some stderr! >&2"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "some stderr!\n")
}

func (s *execSuite) TestCombinedStderr(c *C) {
	outBuf := &bytes.Buffer{}
	opts := &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR! >&2"},
		Stdout:  outBuf,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)
	err = process.Wait()
	c.Check(err, IsNil)
	c.Check(outBuf.String(), Equals, "OUT\nERR!\n")
}

func (s *execSuite) TestEnvironment(c *C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment: map[string]string{"FOO": "bar"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "FOO=bar\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestEnvironmentInheritedFromDaemon(c *C) {
	restore := fakeEnv("FOO", "bar")
	defer restore()

	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo FOO=$FOO"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "FOO=bar\n")
	c.Check(stderr, Equals, "")

	// Check that requested environment takes precedence.
	stdout, stderr, waitErr = s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment: map[string]string{"FOO": "foo"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "FOO=foo\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestWorkingDir(c *C) {
	workingDir := c.MkDir()
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: workingDir,
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, workingDir+"\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestWorkingDirDoesNotExist(c *C) {
	_, err := s.client.Exec(&client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: "/non/existent",
	})
	c.Check(err, ErrorMatches, `.*working directory.*does not exist`)
}

func (s *execSuite) TestWorkingDirNotADirectory(c *C) {
	path := filepath.Join(c.MkDir(), "test")
	err := os.WriteFile(path, nil, 0o777)
	c.Assert(err, IsNil)
	_, err = s.client.Exec(&client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: path,
	})
	c.Check(err, ErrorMatches, `.*working directory.*not a directory`)
}

func (s *execSuite) TestExitError(c *C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR >&2; exit 42"},
	})
	c.Check(waitErr.Error(), Equals, "exit status 42")
	exitCode := 0
	if exitError, ok := waitErr.(*client.ExitError); ok {
		exitCode = exitError.ExitCode()
	}
	c.Check(exitCode, Equals, 42)
	c.Check(stdout, Equals, "OUT\n")
	c.Check(stderr, Equals, "ERR\n")
}

func (s *execSuite) TestTimeout(c *C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Timeout: 10 * time.Millisecond,
	})
	c.Check(waitErr, ErrorMatches, `cannot perform the following tasks:\n.*timed out after 10ms.*`)
	c.Check(stdout, Equals, "")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestContextNoOverrides(c *C) {
	dir := c.MkDir()
	err := s.daemon.overlord.PlanManager().AppendLayer(&plan.Layer{
		Label: "layer1",
		Services: map[string]*plan.Service{"svc1": {
			Name:        "svc1",
			Override:    "replace",
			Command:     "dummy",
			Environment: map[string]string{"FOO": "foo", "BAR": "bar"},
			WorkingDir:  dir,
		}},
	}, false)
	c.Assert(err, IsNil)

	stdout, stderr, err := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo FOO=$FOO BAR=$BAR; pwd"},
		ServiceContext: "svc1",
	})
	c.Assert(err, IsNil)
	c.Check(stdout, Equals, "FOO=foo BAR=bar\n"+dir+"\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestContextOverrides(c *C) {
	err := s.daemon.overlord.PlanManager().AppendLayer(&plan.Layer{
		Label: "layer1",
		Services: map[string]*plan.Service{"svc1": {
			Name:        "svc1",
			Override:    "replace",
			Command:     "dummy",
			Environment: map[string]string{"FOO": "foo", "BAR": "bar"},
			WorkingDir:  c.MkDir(),
		}},
	}, false)
	c.Assert(err, IsNil)

	overrideDir := c.MkDir()
	stdout, stderr, err := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo FOO=$FOO BAR=$BAR; pwd"},
		ServiceContext: "svc1",
		Environment:    map[string]string{"FOO": "oof"},
		WorkingDir:     overrideDir,
	})
	c.Assert(err, IsNil)
	c.Check(stdout, Equals, "FOO=oof BAR=bar\n"+overrideDir+"\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestCurrentUserGroup(c *C) {
	current, err := user.Current()
	c.Assert(err, IsNil)
	group, err := user.LookupGroupId(current.Gid)
	c.Assert(err, IsNil)
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		User:    current.Username,
		Group:   group.Name,
	})
	c.Assert(waitErr, IsNil)
	c.Check(stdout, Equals, current.Username+"\n"+group.Name+"\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) TestUserGroup(c *C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		User:    username,
		Group:   group,
	})
	c.Assert(waitErr, IsNil)
	c.Check(stdout, Equals, username+"\n"+group+"\n")
	c.Check(stderr, Equals, "")

	_, err := s.client.Exec(&client.ExecOptions{
		Command:     []string{"pwd"},
		Environment: map[string]string{"HOME": "/non/existent"},
		User:        username,
		Group:       group,
	})
	c.Assert(err, ErrorMatches, `.*home directory.*does not exist`)
}

func (s *execSuite) TestUserIDGroupID(c *C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	c.Assert(err, IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, IsNil)
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		UserID:  &uid,
		GroupID: &gid,
	})
	c.Assert(waitErr, IsNil)
	c.Check(stdout, Equals, username+"\n"+group+"\n")
	c.Check(stderr, Equals, "")
}

func (s *execSuite) exec(c *C, stdin string, opts *client.ExecOptions) (stdout, stderr string, waitErr error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	opts.Stdin = strings.NewReader(stdin)
	opts.Stdout = outBuf
	opts.Stderr = errBuf
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)
	waitErr = process.Wait()
	return outBuf.String(), errBuf.String(), waitErr
}

func (s *execSuite) TestSignal(c *C) {
	opts := &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, IsNil)

	err = process.SendSignal("SIGINT")
	c.Assert(err, IsNil)

	err = process.Wait()
	c.Check(err, NotNil)

	exitCode := 0
	if exitError, ok := err.(*client.ExitError); ok {
		exitCode = exitError.ExitCode()
	}
	c.Check(exitCode, Equals, 130)
}

func (s *execSuite) TestStreaming(c *C) {
	stdinCh := make(chan []byte)
	stdoutCh := make(chan []byte)
	opts := &client.ExecOptions{
		Command: []string{"cat"},
		Stdin:   channelReader{stdinCh},
		Stdout:  channelWriter{stdoutCh},
		Stderr:  io.Discard,
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

	err = process.Wait()
	c.Check(err, IsNil)
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
	c.Check(execResp.Result["message"], Matches, "cannot find executable .*")
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
	StatusCode int            `json:"status-code"`
	Type       string         `json:"type"`
	Change     string         `json:"change"`
	Result     map[string]any `json:"result"`
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
		Terminal:    opts.Terminal,
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
