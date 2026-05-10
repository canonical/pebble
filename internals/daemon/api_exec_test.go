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
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
)

func TestExecSuite(t *testing.T) {
	tc.Run(t, &execSuite{})
}

type execSuite struct {
	daemon *Daemon
	client *client.Client
}

func (s *execSuite) SetUpSuite(c *tc.C) {
	logger.SetLogger(logger.New(os.Stderr, "[test] "))
}

func (s *execSuite) SetUpTest(c *tc.C) {
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	socketPath := c.MkDir() + ".pebble.socket"
	daemon, err := New(&Options{
		Dir:        c.MkDir(),
		SocketPath: socketPath,
	})
	c.Assert(err, tc.ErrorIsNil)
	err = daemon.Init()
	c.Assert(err, tc.ErrorIsNil)
	daemon.Start()
	s.daemon = daemon

	s.client, err = client.New(&client.Config{Socket: socketPath})
	c.Assert(err, tc.ErrorIsNil)
}

func (s *execSuite) TearDownTest(c *tc.C) {
	err := s.daemon.Stop(nil)
	c.Assert(err, tc.ErrorIsNil)

	err = reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

// Some of these tests use the Go client for simplicity.

func (s *execSuite) TestStdinStdout(c *tc.C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	stdout, stderr, waitErr := s.exec(c, "foo bar", &client.ExecOptions{
		Command: []string{"cat"},
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, "foo bar")
	c.Check(stderr, tc.Equals, "")

	ensureSecurityLog(c, logBuf.String(), "WARN", fmt.Sprintf("authz_admin:%d,exec", os.Getuid()), "Executing command cat")
}

func (s *execSuite) TestStderr(c *tc.C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo some stderr! >&2"},
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, "")
	c.Check(stderr, tc.Equals, "some stderr!\n")
}

func (s *execSuite) TestCombinedStderr(c *tc.C) {
	outBuf := &bytes.Buffer{}
	opts := &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR! >&2"},
		Stdout:  outBuf,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, tc.ErrorIsNil)
	err = process.Wait()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(outBuf.String(), tc.Equals, "OUT\nERR!\n")
}

func (s *execSuite) TestEnvironment(c *tc.C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment: map[string]string{"FOO": "bar"},
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, "FOO=bar\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestEnvironmentInheritedFromDaemon(c *tc.C) {
	restore := fakeEnv("FOO", "bar")
	defer restore()

	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo FOO=$FOO"},
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, "FOO=bar\n")
	c.Check(stderr, tc.Equals, "")

	// Check that requested environment takes precedence.
	stdout, stderr, waitErr = s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo FOO=$FOO"},
		Environment: map[string]string{"FOO": "foo"},
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, "FOO=foo\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestWorkingDir(c *tc.C) {
	workingDir := c.MkDir()
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: workingDir,
	})
	c.Check(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, workingDir+"\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestWorkingDirDoesNotExist(c *tc.C) {
	_, err := s.client.Exec(&client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: "/non/existent",
	})
	c.Check(err, tc.ErrorMatches, `.*working directory.*does not exist`)
}

func (s *execSuite) TestWorkingDirNotADirectory(c *tc.C) {
	path := filepath.Join(c.MkDir(), "test")
	err := os.WriteFile(path, nil, 0o777)
	c.Assert(err, tc.ErrorIsNil)
	_, err = s.client.Exec(&client.ExecOptions{
		Command:    []string{"pwd"},
		WorkingDir: path,
	})
	c.Check(err, tc.ErrorMatches, `.*working directory.*not a directory`)
}

func (s *execSuite) TestExitError(c *tc.C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR >&2; exit 42"},
	})
	c.Check(waitErr.Error(), tc.Equals, "exit status 42")
	exitCode := 0
	if exitError, ok := waitErr.(*client.ExitError); ok {
		exitCode = exitError.ExitCode()
	}
	c.Check(exitCode, tc.Equals, 42)
	c.Check(stdout, tc.Equals, "OUT\n")
	c.Check(stderr, tc.Equals, "ERR\n")
}

func (s *execSuite) TestTimeout(c *tc.C) {
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Timeout: 10 * time.Millisecond,
	})
	c.Check(waitErr, tc.ErrorMatches, `cannot perform the following tasks:\n.*timed out after 10ms.*`)
	c.Check(stdout, tc.Equals, "")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestContextNoOverrides(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)

	stdout, stderr, err := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo FOO=$FOO BAR=$BAR; pwd"},
		ServiceContext: "svc1",
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(stdout, tc.Equals, "FOO=foo BAR=bar\n"+dir+"\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestContextOverrides(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)

	overrideDir := c.MkDir()
	stdout, stderr, err := s.exec(c, "", &client.ExecOptions{
		Command:        []string{"/bin/sh", "-c", "echo FOO=$FOO BAR=$BAR; pwd"},
		ServiceContext: "svc1",
		Environment:    map[string]string{"FOO": "oof"},
		WorkingDir:     overrideDir,
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(stdout, tc.Equals, "FOO=oof BAR=bar\n"+overrideDir+"\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestCurrentUserGroup(c *tc.C) {
	current, err := user.Current()
	c.Assert(err, tc.ErrorIsNil)
	group, err := user.LookupGroupId(current.Gid)
	c.Assert(err, tc.ErrorIsNil)
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		User:    current.Username,
		Group:   group.Name,
	})
	c.Assert(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, current.Username+"\n"+group.Name+"\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) TestUserGroup(c *tc.C) {
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
	c.Assert(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, username+"\n"+group+"\n")
	c.Check(stderr, tc.Equals, "")

	_, err := s.client.Exec(&client.ExecOptions{
		Command:     []string{"pwd"},
		Environment: map[string]string{"HOME": "/non/existent"},
		User:        username,
		Group:       group,
	})
	c.Assert(err, tc.ErrorMatches, `.*home directory.*does not exist`)
}

func (s *execSuite) TestUserIDGroupID(c *tc.C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	c.Assert(err, tc.ErrorIsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, tc.ErrorIsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, tc.ErrorIsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, tc.ErrorIsNil)
	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		UserID:  &uid,
		GroupID: &gid,
	})
	c.Assert(waitErr, tc.IsNil)
	c.Check(stdout, tc.Equals, username+"\n"+group+"\n")
	c.Check(stderr, tc.Equals, "")
}

func (s *execSuite) exec(c *tc.C, stdin string, opts *client.ExecOptions) (stdout, stderr string, waitErr error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	opts.Stdin = strings.NewReader(stdin)
	opts.Stdout = outBuf
	opts.Stderr = errBuf
	process, err := s.client.Exec(opts)
	c.Assert(err, tc.ErrorIsNil)
	waitErr = process.Wait()
	return outBuf.String(), errBuf.String(), waitErr
}

func (s *execSuite) TestSignal(c *tc.C) {
	opts := &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, tc.ErrorIsNil)

	err = process.SendSignal("SIGINT")
	c.Assert(err, tc.ErrorIsNil)

	err = process.Wait()
	c.Check(err, tc.NotNil)

	exitCode := 0
	if exitError, ok := err.(*client.ExitError); ok {
		exitCode = exitError.ExitCode()
	}
	c.Check(exitCode, tc.Equals, 130)
}

func (s *execSuite) TestStreaming(c *tc.C) {
	stdinCh := make(chan []byte)
	stdoutCh := make(chan []byte)
	opts := &client.ExecOptions{
		Command: []string{"cat"},
		Stdin:   channelReader{stdinCh},
		Stdout:  channelWriter{stdoutCh},
		Stderr:  io.Discard,
	}
	process, err := s.client.Exec(opts)
	c.Assert(err, tc.ErrorIsNil)

	for i := range 20 {
		chunk := fmt.Sprintf("chunk %d ", i)
		select {
		case stdinCh <- []byte(chunk):
		case <-time.After(time.Second):
			c.Fatalf("timed out waiting to write to stdin")
		}
		select {
		case b := <-stdoutCh:
			c.Check(string(b), tc.Equals, chunk)
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
	c.Assert(err, tc.ErrorIsNil)
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

func (s *execSuite) TestNoCommand(c *tc.C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{})
	c.Check(httpResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.Type, tc.Equals, "error")
	c.Check(execResp.Result["message"], tc.Equals, "must specify command")
}

func (s *execSuite) TestCommandNotFound(c *tc.C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"badcmd"},
	})
	c.Check(httpResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.Type, tc.Equals, "error")
	c.Check(execResp.Result["message"], tc.Matches, "cannot find executable .*")
}

func (s *execSuite) TestUserGroupError(c *tc.C) {
	gid := os.Getgid()
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"echo", "foo"},
		GroupID: &gid,
	})
	c.Check(httpResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.StatusCode, tc.Equals, http.StatusBadRequest)
	c.Check(execResp.Type, tc.Equals, "error")
	c.Check(execResp.Result["message"], tc.Matches, ".*must specify user, not just group.*")
}

// TestExecChangeReady simulates the scenario where the change is ready before the websocket
// connection is established, so the connection should fail.
func (s *execSuite) TestExecChangeReady(c *tc.C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"echo", "foo"},
	})
	c.Assert(httpResp.StatusCode, tc.Equals, http.StatusAccepted)

	changeID := execResp.Change
	c.Assert(changeID, tc.Not(tc.Equals), "")

	st := s.daemon.overlord.State()
	st.Lock()
	change := st.Change(changeID)
	c.Assert(change, tc.NotNil)
	c.Assert(len(change.Tasks()), tc.Equals, 1)
	// Set the change as failed and set the error on the task.
	change.SetStatus(state.ErrorStatus)
	change.Tasks()[0].Errorf("something went wrong")
	change.Tasks()[0].SetStatus(state.ErrorStatus)
	st.Unlock()

	taskID, ok := execResp.Result["task-id"].(string)
	c.Assert(ok, tc.Equals, true)

	vars := map[string]string{"task-id": taskID, "websocket-id": "control"}
	restoreMuxVars := FakeMuxVars(func(*http.Request) map[string]string {
		return vars
	})
	defer restoreMuxVars()

	websocketCmd := apiCmd("/v1/tasks/{task-id}/websocket/{websocket-id}")
	req, err := http.NewRequest("GET", fmt.Sprintf("/v1/tasks/%s/websocket/%s", taskID, "control"), nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp := v1GetTaskWebsocket(websocketCmd, req, nil).(websocketResponse)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 500)
	c.Check(rec.Body.String(), tc.Matches, `.*something went wrong.*`)
}

type execResponse struct {
	StatusCode int            `json:"status-code"`
	Type       string         `json:"type"`
	Change     string         `json:"change"`
	Result     map[string]any `json:"result"`
}

// execRequest directly calls exec via the ServeHTTP endpoint, rather than
// using the Go client.
func execRequest(c *tc.C, opts *client.ExecOptions) (*http.Response, execResponse) {
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
	c.Assert(err, tc.ErrorIsNil)

	httpResp, body := doRequest(c, v1PostExec, "POST", "/v1/exec", nil, nil, requestBody)
	var execResp execResponse
	err = json.Unmarshal(body.Bytes(), &execResp)
	c.Assert(err, tc.ErrorIsNil)
	return httpResp, execResp
}
