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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

var _ = Suite(&execSuite{})

type execSuite struct {
	daemon     *Daemon
	client     *client.Client
	socketPath string
}

func (s *execSuite) SetUpSuite(c *C) {
	logger.SetLogger(logger.New(os.Stderr, "[test] "))
}

func (s *execSuite) SetUpTest(c *C) {
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
	c.Assert(err, IsNil)
	err = daemon.Init()
	c.Assert(err, IsNil)
	daemon.Start()
	s.daemon = daemon

	s.socketPath = socketPath
	s.client, err = client.New(&client.Config{Socket: socketPath})
	c.Assert(err, IsNil)
}

func (s *execSuite) TearDownTest(c *C) {
	s.client.CloseIdleConnections()
	err := s.daemon.Stop(nil)
	c.Check(err, IsNil)

	err = reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

// Some of these tests use the Go client for simplicity.

func (s *execSuite) TestStdinStdout(c *C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	stdout, stderr, waitErr := s.exec(c, "foo bar", &client.ExecOptions{
		Command: []string{"cat"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "foo bar")
	c.Check(stderr, Equals, "")

	ensureSecurityLog(c, logBuf.String(), "WARN", fmt.Sprintf("authz_admin:%d,exec", os.Getuid()), "Executing command cat")
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

func (s *execSuite) TestEnvironmentTraceContext(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	// The daemon's own trace context isn't inherited.
	restore := fakeEnv("TRACEPARENT", "00-11111111111111111111111111111111-2222222222222222-01")
	defer restore()

	// The command continues the caller's trace, as a descendant of the exec
	// task's span.
	caller := tracing.NewSpanContext(tracing.SpanContextConfig{
		TraceID:    tracing.TraceID{0xab},
		SpanID:     tracing.SpanID{0xcd},
		TraceFlags: tracing.FlagsSampled,
		Remote:     true,
	})
	var err error
	s.client.CloseIdleConnections()
	s.client, err = client.New(&client.Config{Socket: s.socketPath, SpanContext: caller})
	c.Assert(err, IsNil)

	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo $TRACEPARENT"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Matches, "00-"+caller.TraceID().String()+"-[0-9a-f]{16}-01\n")
	c.Check(strings.Contains(stdout, caller.SpanID().String()), Equals, false)
	c.Check(stderr, Equals, "")

	// Requested environment takes precedence.
	stdout, _, waitErr = s.exec(c, "", &client.ExecOptions{
		Command:     []string{"/bin/sh", "-c", "echo $TRACEPARENT"},
		Environment: map[string]string{"TRACEPARENT": "explicit"},
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "explicit\n")
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
	err := s.daemon.overlord.PlanManager().AppendLayer(context.Background(), &plan.Layer{
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
	err := s.daemon.overlord.PlanManager().AppendLayer(context.Background(), &plan.Layer{
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

// TestStopWhileExecRunning is a regression test for
// https://github.com/canonical/pebble/issues/682: stopping the daemon while
// an exec command is running used to take about a second (the HTTP server's
// shutdown timeout), because the long-poll GET /v1/changes/{id}/wait request
// used by "pebble exec" only becomes idle once the running command is
// killed, which happens in Overlord.Stop -- but that was called after
// Server.Shutdown, so Shutdown always ran out its full timeout.
func (s *execSuite) TestStopWhileExecRunning(c *C) {
	process, err := s.client.Exec(&client.ExecOptions{
		Command: []string{"sleep", "30"},
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	c.Assert(err, IsNil)

	// Wait for the change to be running before stopping the daemon, so the
	// long-poll GET /v1/changes/{id}/wait request is definitely in flight.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- process.Wait()
	}()
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	err = s.daemon.Stop(nil)
	elapsed := time.Since(start)
	c.Check(err, IsNil)
	c.Check(elapsed < 500*time.Millisecond, Equals, true,
		Commentf("Daemon.Stop took %s with an exec command running", elapsed))

	select {
	case err := <-waitDone:
		c.Check(err, NotNil) // process was killed, so it's not a clean exit
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for exec process to finish")
	}
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

	for i := range 20 {
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

// TestExecChangeReady simulates the scenario where the change is ready before the websocket
// connection is established, so the connection should fail.
func (s *execSuite) TestExecChangeReady(c *C) {
	httpResp, execResp := execRequest(c, &client.ExecOptions{
		Command: []string{"echo", "foo"},
	})
	c.Assert(httpResp.StatusCode, Equals, http.StatusAccepted)

	changeID := execResp.Change
	c.Assert(changeID, Not(Equals), "")

	st := s.daemon.overlord.State()
	st.Lock()
	change := st.Change(changeID)
	c.Assert(change, NotNil)
	c.Assert(len(change.Tasks()), Equals, 1)
	// Set the change as failed and set the error on the task.
	change.SetStatus(state.ErrorStatus)
	change.Tasks()[0].Errorf("something went wrong")
	change.Tasks()[0].SetStatus(state.ErrorStatus)
	st.Unlock()

	taskID, ok := execResp.Result["task-id"].(string)
	c.Assert(ok, Equals, true)

	websocketCmd := apiCmd("/v1/tasks/{taskID}/websocket/{websocketID}")
	req, err := http.NewRequest("GET", fmt.Sprintf("/v1/tasks/%s/websocket/%s", taskID, "control"), nil)
	c.Assert(err, IsNil)
	req.SetPathValue("taskID", taskID)
	req.SetPathValue("websocketID", "control")
	rsp := v1GetTaskWebsocket(websocketCmd, req, nil).(websocketResponse)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, Equals, 500)
	c.Check(rec.Body.String(), Matches, `.*something went wrong.*`)
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

// waitSpan waits for a span with the given name to end, as some spans (such
// as a task's span) end shortly after the client sees the result.
func waitSpan(c *C, recorder *tracingtest.Recorder, name string) tracingtest.ReadOnlySpan {
	timeout := time.After(5 * time.Second)
	for {
		for _, span := range recorder.Ended() {
			if span.Name() == name {
				return span
			}
		}
		select {
		case <-timeout:
			c.Fatalf("timed out waiting for %q span (have %v)", name, spanNames(recorder.Ended()))
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// findSpans returns the ended spans with the given name.
func findSpans(spans []tracingtest.ReadOnlySpan, name string) []tracingtest.ReadOnlySpan {
	var found []tracingtest.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == name {
			found = append(found, span)
		}
	}
	return found
}

func (s *execSuite) TestTracing(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	// The requests are part of the caller's trace.
	caller := tracing.NewSpanContext(tracing.SpanContextConfig{
		TraceID:    tracing.TraceID{0xab},
		SpanID:     tracing.SpanID{0xcd},
		TraceFlags: tracing.FlagsSampled,
		Remote:     true,
	})
	s.client.SetSpanContext(caller)

	stdout, stderr, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "echo OUT; echo ERR >&2; exit 3"},
	})
	c.Check(waitErr, ErrorMatches, "exit status 3")
	c.Check(stdout, Equals, "OUT\n")
	c.Check(stderr, Equals, "ERR\n")

	// The exec task's span records the command's lifecycle.
	task := waitSpan(c, recorder, "do exec")
	c.Check(task.SpanContext().TraceID(), Equals, caller.TraceID())
	// A non-zero exit code isn't a failure of the task.
	c.Check(task.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(task)
	c.Check(attrs["process.executable.name"].AsString(), Equals, "sh")
	c.Check(attrs["process.pid"].AsInt64() > 0, Equals, true)
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(3))
	c.Check(attrs["pebble.exec.terminal"].AsBool(), Equals, false)
	c.Check(attrs["pebble.exec.interactive"].AsBool(), Equals, false)
	for _, key := range []string{"pebble.exec.timeout", "pebble.exec.timed-out"} {
		_, has := attrs[key]
		c.Check(has, Equals, false, Commentf("unexpected attribute %s", key))
	}
	// The control loop runs concurrently with the command, so its event's
	// position isn't fixed.
	var events []string
	for _, name := range eventNames(task) {
		if name != "control connected" {
			events = append(events, name)
		}
	}
	c.Check(events, DeepEquals, []string{"io connected", "process started", "process exited", "output sent"})
	c.Check(len(eventNames(task)), Equals, 5, Commentf("%v", eventNames(task)))

	// The task is part of the exec change, created by the exec request.
	change := waitSpan(c, recorder, "change exec")
	c.Check(task.Parent().SpanID(), Equals, change.SpanContext().SpanID())
	changeID := spanAttrs(change)["pebble.change.id"].AsString()
	c.Check(changeID, Not(Equals), "")
	execReq := findSpan(c, recorder.Ended(), "POST /v1/exec")
	c.Check(change.Parent().SpanID(), Equals, execReq.SpanContext().SpanID())
	c.Check(execReq.Parent().SpanID(), Equals, caller.SpanID())
	c.Check(execReq.Status().Code, Equals, tracing.StatusUnset)
	execAttrs := spanAttrs(execReq)
	c.Check(execAttrs["http.response.status_code"].AsInt64(), Equals, int64(202))
	c.Check(execAttrs["pebble.change.id"].AsString(), Equals, changeID)
	c.Check(execAttrs["pebble.user.access"].AsString(), Equals, "admin")
	c.Check(execAttrs["user.id"].AsString(), Equals, strconv.Itoa(os.Getuid()))

	// The websocket requests (stdio, stderr and control) are upgraded, so
	// their status is 101, and they're linked to the change.
	websockets := findSpans(recorder.Ended(), "GET /v1/tasks/{taskID}/websocket/{websocketID}")
	c.Assert(websockets, HasLen, 3)
	var websocketIDs []string
	for _, ws := range websockets {
		c.Check(ws.Parent().SpanID(), Equals, caller.SpanID())
		c.Check(ws.Status().Code, Equals, tracing.StatusUnset)
		wsAttrs := spanAttrs(ws)
		c.Check(wsAttrs["http.response.status_code"].AsInt64(), Equals, int64(101))
		c.Check(wsAttrs["pebble.task.id"].AsString(), Equals, spanAttrs(task)["pebble.task.id"].AsString())
		c.Check(wsAttrs["pebble.change.id"].AsString(), Equals, changeID)
		websocketIDs = append(websocketIDs, wsAttrs["pebble.websocket.id"].AsString())
		c.Assert(ws.Links(), HasLen, 1)
		c.Check(ws.Links()[0].SpanContext.SpanID(), Equals, change.SpanContext().SpanID())
	}
	sort.Strings(websocketIDs)
	c.Check(websocketIDs, DeepEquals, []string{"control", "stderr", "stdio"})

	// The client waited for the change to finish.
	wait := findSpan(c, recorder.Ended(), "GET /v1/changes/{id}/wait")
	c.Check(wait.Parent().SpanID(), Equals, caller.SpanID())
	c.Check(wait.Status().Code, Equals, tracing.StatusUnset)
	waitAttrs := spanAttrs(wait)
	c.Check(waitAttrs["pebble.change.id"].AsString(), Equals, changeID)
	c.Check(waitAttrs["pebble.wait.outcome"].AsString(), Equals, "ready")
	c.Check(waitAttrs["pebble.change.status"].AsString(), Equals, "Done")
	c.Assert(wait.Links(), HasLen, 1)
	c.Check(wait.Links()[0].SpanContext.SpanID(), Equals, change.SpanContext().SpanID())

	// Exec requires admin access, so each request authenticates the caller.
	auths := findSpans(recorder.Ended(), "authenticate")
	c.Check(len(auths) >= 5, Equals, true, Commentf("%v", spanNames(recorder.Ended())))
	for _, auth := range auths {
		c.Check(spanAttrs(auth)["pebble.auth.method"].AsString(), Equals, "ucred")
	}
}

func (s *execSuite) TestTracingTimeout(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	_, _, waitErr := s.exec(c, "", &client.ExecOptions{
		Command: []string{"sleep", "1"},
		Timeout: 10 * time.Millisecond,
	})
	c.Check(waitErr, ErrorMatches, `cannot perform the following tasks:\n.*timed out after 10ms.*`)

	task := waitSpan(c, recorder, "do exec")
	c.Check(task.Status().Code, Equals, tracing.StatusError)
	c.Check(task.Status().Description, Matches, "timed out after 10ms.*")
	attrs := spanAttrs(task)
	c.Check(attrs["process.executable.name"].AsString(), Equals, "sleep")
	c.Check(attrs["pebble.exec.timeout"].AsString(), Equals, "10ms")
	c.Check(attrs["pebble.exec.timed-out"].AsBool(), Equals, true)
	// The process was killed (SIGKILL) when the timeout expired.
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(137))
	// The timeout error is recorded as an exception event by the task runner.
	var events []string
	for _, name := range eventNames(task) {
		if name != "control connected" {
			events = append(events, name)
		}
	}
	c.Check(events, DeepEquals, []string{"io connected", "process started", "process exited", "output sent", "exception"})
}

func (s *execSuite) TestTracingSignal(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	process, err := s.client.Exec(&client.ExecOptions{
		Command: []string{"sleep", "10"},
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	c.Assert(err, IsNil)
	err = process.SendSignal("SIGINT")
	c.Assert(err, IsNil)
	err = process.Wait()
	c.Check(err, ErrorMatches, "exit status 130")

	task := waitSpan(c, recorder, "do exec")
	c.Check(spanAttrs(task)["process.exit.code"].AsInt64(), Equals, int64(130))
	events := eventNames(task)
	c.Check(events, DeepEquals, []string{
		"io connected", "process started", "control connected", "signal", "process exited", "output sent",
	})
	for _, event := range task.Events() {
		if event.Name != "signal" {
			continue
		}
		var attrs []string
		for _, kv := range event.Attributes {
			attrs = append(attrs, string(kv.Key)+"="+kv.Value.AsString())
		}
		c.Check(attrs, DeepEquals, []string{"pebble.exec.signal=SIGINT"})
	}
}

func (s *execSuite) TestTracingTerminal(c *C) {
	recorder := tracingtest.NewRecorder()
	defer recorder.Restore()

	stdout, _, waitErr := s.exec(c, "", &client.ExecOptions{
		Command:  []string{"/bin/echo", "hello"},
		Terminal: true,
	})
	c.Check(waitErr, IsNil)
	c.Check(stdout, Equals, "hello\r\n")

	task := waitSpan(c, recorder, "do exec")
	c.Check(task.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(task)
	c.Check(attrs["process.executable.name"].AsString(), Equals, "echo")
	c.Check(attrs["pebble.exec.terminal"].AsBool(), Equals, true)
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(0))
}
