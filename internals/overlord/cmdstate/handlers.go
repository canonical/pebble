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

package cmdstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/ptyutil"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/wsutil"
)

const (
	connectTimeout   = 5 * time.Second
	handshakeTimeout = 5 * time.Second
	waitDelay        = time.Second

	wsControl = "control"
	wsStdio   = "stdio"
	wsStderr  = "stderr"
)

// execution tracks the execution of a command.
type execution struct {
	command     []string
	environment map[string]string
	timeout     time.Duration
	terminal    bool
	interactive bool
	splitStderr bool
	width       int
	height      int
	userID      *int
	groupID     *int
	workingDir  string

	websockets       map[string]*websocket.Conn
	websocketsLock   sync.Mutex
	ioConnected      chan struct{}
	controlConnected chan struct{}
}

func (m *CommandManager) doExec(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	setupObj := st.Cached(execSetupKey{task.ID()})
	st.Unlock()
	setup, ok := setupObj.(*execSetup)
	if !ok || setup == nil {
		return fmt.Errorf("internal error: cannot get exec setup object for task %q", task.ID())
	}

	// Set up the object that will track the execution.
	e := &execution{
		command:          setup.Command,
		environment:      setup.Environment,
		timeout:          setup.Timeout,
		terminal:         setup.Terminal,
		interactive:      setup.Interactive,
		splitStderr:      setup.SplitStderr,
		width:            setup.Width,
		height:           setup.Height,
		userID:           setup.UserID,
		groupID:          setup.GroupID,
		workingDir:       setup.WorkingDir,
		websockets:       make(map[string]*websocket.Conn),
		ioConnected:      make(chan struct{}),
		controlConnected: make(chan struct{}),
	}

	// Populate the websockets map (with nil connections until connected).
	e.websockets[wsControl] = nil
	e.websockets[wsStdio] = nil
	if e.splitStderr {
		e.websockets[wsStderr] = nil
	}

	// Store the execution object on the manager (for Connect).
	m.executionsMutex.Lock()
	m.executions[task.ID()] = e
	m.executionsMutex.Unlock()
	m.executionsCond.Broadcast() // signal that Connects can start happening
	defer func() {
		m.executionsMutex.Lock()
		delete(m.executions, task.ID())
		m.executionsMutex.Unlock()
	}()

	// Run the command! Killing the tomb will terminate the command.
	ctx := tomb.Context(context.Background())
	return e.do(ctx, task)
}

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin:      func(r *http.Request) bool { return true },
	HandshakeTimeout: handshakeTimeout,
}

func (e *execution) connect(r *http.Request, w http.ResponseWriter, id string) error {
	e.websocketsLock.Lock()
	conn, ok := e.websockets[id]
	e.websocketsLock.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	if conn != nil {
		return fmt.Errorf("websocket %q already connected", id)
	}

	// Upgrade the HTTP connection to a websocket connection.
	conn, err := websocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	// Save the connection.
	e.websocketsLock.Lock()
	defer e.websocketsLock.Unlock()
	e.websockets[id] = conn

	// Signal that we're connected.
	if id == wsControl {
		close(e.controlConnected)
	} else if e.websockets[wsStdio] != nil && (!e.splitStderr || e.websockets[wsStderr] != nil) {
		close(e.ioConnected)
	}
	return nil
}

func (e *execution) getWebsocket(key string) *websocket.Conn {
	e.websocketsLock.Lock()
	defer e.websocketsLock.Unlock()
	return e.websockets[key]
}

// waitIOConnected waits till all the I/O websockets are connected or the
// connect timeout elapses (or the provided ctx is cancelled).
func (e *execution) waitIOConnected(ctx context.Context, execID string) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Noticef("Exec %s: timeout waiting for websocket connections: %v", execID, ctx.Err())
			return fmt.Errorf("exec %s: timeout waiting for websocket connections: %w", execID, ctx.Err())
		}
		return ctx.Err()
	case <-e.ioConnected:
		return nil
	}
}

// do actually runs the command.
func (e *execution) do(ctx context.Context, task *state.Task) error {
	// Wait till client has connected to "stdio" websocket (and "stderr" if
	// separating stderr), to avoid race conditions forwarding I/O.
	err := e.waitIOConnected(ctx, task.ID())
	if err != nil {
		return err
	}

	// Files/pipes to close before and after waiting for output to be finished sending.
	var beforeClosers []io.Closer
	var afterClosers []io.Closer

	// Stdin/stdout/stderr for the exec.Cmd process.
	var stdin *os.File
	var stdout *os.File
	var stderr *os.File

	// Closed to make the controlLoop stop early.
	stopControl := make(chan struct{})
	defer close(stopControl)

	pidCh := make(chan int)
	childDead := make(chan struct{})
	var wgOutputSent sync.WaitGroup

	if e.terminal {
		var uid, gid int
		if e.userID != nil && e.groupID != nil {
			uid, gid = *e.userID, *e.groupID
		} else {
			uid = os.Getuid()
			gid = os.Getgid()
		}

		master, slave, err := ptyutil.OpenPty(int64(uid), int64(gid))
		if err != nil {
			return err
		}
		afterClosers = append(afterClosers, master)
		beforeClosers = append(beforeClosers, slave)

		stdin = slave // stdin will be overwritten below if interactive is true
		stdout = slave
		stderr = slave // stderr will be overwritten below if splitStderr is true

		if e.width > 0 && e.height > 0 {
			err = ptyutil.SetSize(int(master.Fd()), e.width, e.height)
			if err != nil {
				logger.Noticef("Exec %s: cannot set initial terminal size to %dx%d: %v",
					task.ID(), e.width, e.height, err)
			}
		}

		go e.controlLoop(task.ID(), pidCh, stopControl, int(master.Fd()))

		// Start goroutine to mirror PTY output to "stdio" websocket.
		ioConn := e.getWebsocket(wsStdio)
		wgOutputSent.Add(1)
		go func() {
			defer wgOutputSent.Done()

			logger.Debugf("Exec %s: started mirroring websocket", task.ID())
			defer logger.Debugf("Exec %s: finished mirroring websocket", task.ID())

			wsutil.MirrorToWebsocket(ioConn, master, childDead, int(master.Fd()))
		}()

		if e.interactive {
			// Interactive: start goroutine to receive stdin from "stdio"
			// websocket and write to the PTY.
			go func() {
				<-wsutil.WebsocketRecvStream(master, ioConn)
				// If the interactive is enforced, it is possible to finish
				// reading earlier than the mirroring go routine sends all the
				// output to the client. Thus, closing the master descriptor
				// here will terminate mirroring prematurely. Instead, we
				// should send Ctrl-D to the fd to indicate the end of input.
				master.Write([]byte{byte(unix.VEOF)})
			}()
		} else {
			// Non-interactive: start goroutine to receive stdin from "stdio"
			// websocket and write to cmd.Stdin pipe.
			stdinReader, stdinWriter, err := os.Pipe()
			if err != nil {
				return err
			}
			stdin = stdinReader
			afterClosers = append(afterClosers, stdinReader)
			go func() {
				<-wsutil.WebsocketRecvStream(stdinWriter, ioConn)
				stdinWriter.Close()
			}()
		}
	} else {
		// No PTY/terminal, all I/O uses pipes.

		go e.controlLoop(task.ID(), pidCh, stopControl, -1)

		// Start goroutine to receive stdin from "stdio" websocket and write to
		// cmd.Stdin pipe.
		ioConn := e.getWebsocket(wsStdio)
		stdinReader, stdinWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		stdin = stdinReader
		afterClosers = append(afterClosers, stdinReader)
		go func() {
			<-wsutil.WebsocketRecvStream(stdinWriter, ioConn)
			stdinWriter.Close()
		}()

		// Start goroutine to receive from cmd.Stdout pipe and write to "stdio"
		// websocket.
		stdoutReader, stdoutWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		beforeClosers = append(beforeClosers, stdoutWriter)
		stdout = stdoutWriter
		stderr = stdoutWriter // stderr will be overwritten below if splitStderr true
		wgOutputSent.Add(1)
		go func() {
			defer wgOutputSent.Done()
			<-wsutil.WebsocketSendStream(ioConn, stdoutReader, -1)
			stdoutReader.Close()
		}()
	}

	if e.splitStderr {
		// Start goroutine to receive from cmd.Stderr pipe and write to a
		// separate "stderr" websocket.
		stderrReader, stderrWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		beforeClosers = append(beforeClosers, stderrWriter)
		stderr = stderrWriter
		stderrConn := e.getWebsocket(wsStderr)
		wgOutputSent.Add(1)
		go func() {
			defer wgOutputSent.Done()
			<-wsutil.WebsocketSendStream(stderrConn, stderrReader, -1)
			stderrReader.Close()
		}()
	}

	if e.timeout != 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, e.command[0], e.command[1:]...)

	// Ensure cmd.Env is not nil (does not inherit parent env). This is not
	// strictly necessary as cmdstate.Exec always sets some environment
	// variables, but code defensively.
	cmd.Env = make([]string, 0, len(e.environment))

	for k, v := range e.environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Dir = e.workingDir

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if e.userID != nil && e.groupID != nil {
		isCurrent, err := osutil.IsCurrent(*e.userID, *e.groupID)
		if err != nil {
			logger.Debugf("Cannot determine if uid %d gid %d is current user", *e.userID, *e.groupID)
		}
		if !isCurrent {
			cmd.SysProcAttr.Credential = &syscall.Credential{
				Uid: uint32(*e.userID),
				Gid: uint32(*e.groupID),
			}
		}
	}

	// Creates a new session if the calling process is not a process group
	// leader. The calling process is the leader of the new session, the
	// process group leader of the new process group, and has no controlling
	// terminal. This is important to allow remote shells to handle Ctrl+C.
	cmd.SysProcAttr.Setsid = true

	// Make the given terminal the controlling terminal of the calling
	// process. The calling process must be a session leader and not have a
	// controlling terminal already. This is important as allows Ctrl+C to
	// work as expected for non-shell programs.
	if e.terminal && e.interactive {
		cmd.SysProcAttr.Setctty = true
	}

	// Start the command!
	err = reaper.StartCommand(cmd)
	exitCode := -1
	if err == nil {
		// Send its PID to the control loop.
		pidCh <- cmd.Process.Pid

		// Wait for it to finish.
		exitCode, err = reaper.WaitCommand(cmd)
	}

	// Close open files and channels.
	for _, closer := range beforeClosers {
		_ = closer.Close()
	}

	// Close the control channel, if connected.
	controlConn := e.getWebsocket(wsControl)
	if controlConn != nil {
		_ = controlConn.Close()
	}

	close(childDead)

	wgOutputSent.Wait()

	for _, closer := range afterClosers {
		_ = closer.Close()
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		setExitCode(task, -1)
		return fmt.Errorf("timed out after %v: %w", e.timeout, ctx.Err())
	}
	if err != nil {
		setExitCode(task, -1)
		return err
	}
	setExitCode(task, exitCode)
	return nil
}

func setExitCode(task *state.Task, exitCode int) {
	st := task.State()
	st.Lock()
	defer st.Unlock()
	task.Set("api-data", map[string]any{
		"exit-code": exitCode,
	})
}

type execCommand struct {
	Command string          `json:"command"`
	Signal  *execSignalArgs `json:"signal"`
	Resize  *execResizeArgs `json:"resize"`
}

type execSignalArgs struct {
	Name string `json:"name"`
}

type execResizeArgs struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (e *execution) controlLoop(execID string, pidCh <-chan int, stop <-chan struct{}, ptyFd int) {
	logger.Debugf("Exec %s: control handler waiting", execID)
	defer logger.Debugf("Exec %s: control handler finished", execID)

	// Wait till we receive the process's PID (command started).
	var pid int
	select {
	case pid = <-pidCh:
		break
	case <-stop:
		return
	}

	// Wait till the control websocket is connected.
	select {
	case <-e.controlConnected:
		break
	case <-stop:
		return
	}

	logger.Debugf("Exec %s: control handler started for PID %d", execID, pid)
	for {
		controlConn := e.getWebsocket(wsControl)
		mt, r, err := controlConn.NextReader()
		if mt == websocket.CloseMessage {
			break
		}

		if err != nil {
			logger.Debugf("Exec %s: cannot get next websocket reader for PID %d: %v", execID, pid, err)
			er, ok := err.(*websocket.CloseError)
			if !ok {
				break
			}
			if er.Code != websocket.CloseAbnormalClosure {
				break
			}

			// If an abnormal closure occurred, kill the attached process.
			err := unix.Kill(pid, unix.SIGKILL)
			if err != nil {
				logger.Noticef("Exec %s: cannot send SIGKILL to pid %d: %v", execID, pid, err)
			} else {
				logger.Debugf("Exec %s: sent SIGKILL to pid %d", execID, pid)
			}
			break
		}

		var command execCommand
		err = json.NewDecoder(r).Decode(&command)
		if err != nil {
			logger.Noticef("Exec %s: cannot decode control websocket command: %v", execID, err)
			continue
		}

		switch {
		case command.Command == "resize" && e.terminal:
			if command.Resize == nil {
				logger.Noticef(`Exec %s: control command "resize" requires terminal width and height`, execID)
				continue
			}
			w, h := command.Resize.Width, command.Resize.Height
			err = ptyutil.SetSize(ptyFd, w, h)
			if err != nil {
				logger.Noticef(`Exec %s: control command "resize" cannot set terminal size to %dx%d: %v`, execID, w, h, err)
				continue
			}
			logger.Debugf(`Exec %s: PID %d terminal resized to %dx%d`, execID, pid, w, h)
		case command.Command == "signal":
			if command.Signal == nil {
				logger.Noticef(`Exec %s: control command "signal" requires signal name`, execID)
				continue
			}
			name := command.Signal.Name
			sig := unix.SignalNum(name)
			if sig == 0 {
				logger.Noticef("Exec %s: invalid signal name %q", execID, name)
				continue
			}
			logger.Debugf(`Exec %s: received control command "signal" with name %q`, execID, name)
			err := unix.Kill(pid, sig)
			if err != nil {
				logger.Noticef(`Exec %s: control command "signal" cannot forward %s to PID %d: %v`, execID, name, pid, err)
				continue
			}
			logger.Noticef("Exec %s: forwarded signal %s to PID %d", execID, name, pid)
		default:
			logger.Noticef("Exec %s: invalid control websocket command %q", execID, command.Command)
		}
	}
}
