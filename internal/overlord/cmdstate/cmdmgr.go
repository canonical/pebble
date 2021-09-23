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
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/ptyutil"
	"github.com/canonical/pebble/internal/strutil"
	"github.com/canonical/pebble/internal/wsutil"
)

const (
	connectTimeout   = 5 * time.Second
	handshakeTimeout = 5 * time.Second

	wsControl = "control"
	wsIO      = "io"
	wsStderr  = "stderr"
)

type CommandManager struct{}

// NewManager creates a new CommandManager.
func NewManager(runner *state.TaskRunner) *CommandManager {
	runner.AddHandler("exec", doExec, nil)
	return &CommandManager{}
}

// Ensure is part of the overlord.StateManager interface.
func (m *CommandManager) Ensure() error {
	return nil
}

// ExecArgs holds the arguments for a command execution.
type ExecArgs struct {
	Command     []string
	Environment map[string]string
	WorkingDir  string
	Timeout     time.Duration
	UserID      *int
	GroupID     *int
	UseTerminal bool
	SplitStderr bool
	Width       int
	Height      int
}

// ExecMetadata is the metadata returned from an Exec call.
type ExecMetadata struct {
	WebsocketIDs map[string]string // keys are "control", "io", and "stderr" if SplitStderr true
	Environment  map[string]string
	WorkingDir   string
}

// Exec creates a change that will execute the command with the given arguments.
func Exec(st *state.State, args *ExecArgs) (*state.Change, ExecMetadata, error) {
	env := map[string]string{}
	for k, v := range args.Environment {
		env[k] = v
	}

	// Set a reasonable default for PATH.
	_, ok := env["PATH"]
	if !ok {
		env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	// Set HOME and USER based on the UserID.
	if env["HOME"] == "" || env["USER"] == "" {
		var userID int
		if args.UserID != nil {
			userID = *args.UserID
		} else {
			userID = os.Getuid()
		}
		u, err := user.LookupId(strconv.Itoa(userID))
		if err != nil {
			logger.Noticef("Failed to look up user %d: %v", userID, err)
		} else {
			if env["HOME"] == "" {
				env["HOME"] = u.HomeDir
			}
			if env["USER"] == "" {
				env["USER"] = u.Username
			}
		}
	}

	// Set default value for LANG.
	_, ok = env["LANG"]
	if !ok {
		env["LANG"] = "C.UTF-8"
	}

	// Set default working directory to $HOME, or / if $HOME not set.
	cwd := args.WorkingDir
	if cwd == "" {
		cwd = env["HOME"]
		if cwd == "" {
			cwd = "/"
		}
	}

	// Set up the object that will track the execution.
	e := &execution{
		command:          args.Command,
		env:              env,
		timeout:          args.Timeout,
		websockets:       make(map[string]*websocket.Conn),
		websocketIDs:     make(map[string]string),
		ioConnected:      make(chan struct{}),
		controlConnected: make(chan struct{}),
		useTerminal:      args.UseTerminal,
		splitStderr:      args.SplitStderr,
		width:            args.Width,
		height:           args.Height,
		uid:              args.UserID,
		gid:              args.GroupID,
		workingDir:       cwd,
	}

	// Generate unique identifier for each websocket (used by connect API).
	e.websockets[wsControl] = nil
	e.websockets[wsIO] = nil
	if args.SplitStderr {
		e.websockets[wsStderr] = nil
	}
	for key := range e.websockets {
		var err error
		e.websocketIDs[key], err = strutil.UUID()
		if err != nil {
			return nil, ExecMetadata{}, err
		}
	}

	// Make a copy of websocketIDs map for the return value.
	ids := make(map[string]string, len(e.websocketIDs))
	for key, id := range e.websocketIDs {
		ids[key] = id
	}
	metadata := ExecMetadata{
		WebsocketIDs: ids,
		Environment:  env,
		WorkingDir:   cwd,
	}

	// Create change object for this execution and store it in state.
	cacheKey, err := strutil.UUID()
	if err != nil {
		return nil, ExecMetadata{}, err
	}
	st.Cache("exec-"+cacheKey, e)
	change := st.NewChange("exec", fmt.Sprintf("Execute command %q", args.Command[0]))
	task := st.NewTask("exec", fmt.Sprintf("exec command %q", args.Command[0]))
	change.AddAll(state.NewTaskSet(task))
	change.Set("cache-key", cacheKey)

	return change, metadata, nil
}

func doExec(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	change := task.Change()
	st.Unlock()

	e, cacheKey, err := getExecutionAndCacheKey(change)
	if err != nil {
		return err
	}

	// Run the command! Killing the tomb will terminate the command.
	ctx := tomb.Context(context.Background())
	err = e.do(ctx, change)

	deleteExecutionFromCache(change, cacheKey)

	return err
}

func getExecutionAndCacheKey(change *state.Change) (*execution, string, error) {
	st := change.State()
	st.Lock()
	defer st.Unlock()

	var cacheKey string
	err := change.Get("cache-key", &cacheKey)
	if err != nil {
		return nil, "", err
	}

	e, ok := st.Cached("exec-" + cacheKey).(*execution)
	if !ok {
		return nil, "", fmt.Errorf("exec for change %q no longer active", change.ID())
	}
	return e, cacheKey, nil
}

func deleteExecutionFromCache(change *state.Change, cacheKey string) {
	st := change.State()
	st.Lock()
	defer st.Unlock()
	st.Cache("exec-"+cacheKey, nil)
}

// Connect upgrades the HTTP connection and connects to the given websocket.
func Connect(r *http.Request, w http.ResponseWriter, change *state.Change, websocketID string) error {
	e, _, err := getExecutionAndCacheKey(change)
	if err != nil {
		return err
	}
	return e.connect(r, w, websocketID)
}

// execution tracks the execution of a command.
type execution struct {
	command          []string
	env              map[string]string
	timeout          time.Duration
	websockets       map[string]*websocket.Conn
	websocketsLock   sync.Mutex
	websocketIDs     map[string]string
	ioConnected      chan struct{}
	controlConnected chan struct{}
	useTerminal      bool
	splitStderr      bool
	width            int
	height           int
	uid              *int
	gid              *int
	workingDir       string
}

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin:      func(r *http.Request) bool { return true },
	HandshakeTimeout: handshakeTimeout,
}

func (e *execution) connect(r *http.Request, w http.ResponseWriter, id string) error {
	// Find websocket key by websocket's unique ID.
	var key, wsID string
	for key, wsID = range e.websocketIDs {
		if id == wsID {
			break
		}
	}
	if id != wsID {
		return fmt.Errorf("websocket ID %q not found", id)
	}

	// Upgrade the HTTP connection to a websocket connection.
	conn, err := websocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	// Save the connection.
	e.websocketsLock.Lock()
	defer e.websocketsLock.Unlock()
	e.websockets[key] = conn

	// Signal that we're connected.
	if key == wsControl {
		close(e.controlConnected)
	} else if e.websockets[wsIO] != nil && (!e.splitStderr || e.websockets[wsStderr] != nil) {
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
func (e *execution) waitIOConnected(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Noticef("Timeout waiting for websocket connections: %v", ctx.Err())
			return fmt.Errorf("timeout waiting for websocket connections: %w", ctx.Err())
		}
		return ctx.Err()
	case <-e.ioConnected:
		return nil
	}
}

// do actually runs the command.
func (e *execution) do(ctx context.Context, change *state.Change) error {
	// Wait till client has connected to "io" websocket (and "stderr" if
	// separating stderr), to avoid race conditions forwarding I/O.
	err := e.waitIOConnected(ctx)
	if err != nil {
		return err
	}

	var ttys []*os.File
	var ptys []*os.File

	var stdin *os.File
	var stdout *os.File
	var stderr *os.File

	controlExit := make(chan struct{})
	defer close(controlExit)

	pidCh := make(chan int)
	childDead := make(chan struct{})
	var wg sync.WaitGroup

	if e.useTerminal {
		var uid, gid int
		if e.uid != nil && e.gid != nil {
			uid, gid = *e.uid, *e.gid
		} else {
			uid = os.Getuid()
			gid = os.Getgid()
		}

		pty, tty, err := ptyutil.OpenPty(int64(uid), int64(gid))
		if err != nil {
			return err
		}
		ptys = append(ptys, pty)
		ttys = append(ttys, tty)

		stdin = tty
		stdout = tty
		stderr = tty // stderr will be overwritten below if splitStderr true

		if e.width > 0 && e.height > 0 {
			ptyutil.SetSize(int(pty.Fd()), e.width, e.height)
		}

		go e.controlLoop(pidCh, controlExit, int(pty.Fd()))

		// Start goroutine to mirror PTY I/O to "io" websocket.
		wg.Add(1)
		go func() {
			logger.Debugf("Started mirroring websocket")
			ioConn := e.getWebsocket(wsIO)
			readDone, writeDone := wsutil.WebsocketExecMirror(
				ioConn, pty, pty, childDead, int(pty.Fd()))
			<-readDone
			<-writeDone
			logger.Debugf("Finished mirroring websocket")

			_ = ioConn.Close()
			wg.Done()
		}()
	} else {
		go e.controlLoop(pidCh, controlExit, -1)

		// Start goroutine to receive stdin from "io" websocket and write to
		// cmd.Stdin pipe.
		ioConn := e.getWebsocket(wsIO)
		stdinReader, stdinWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		stdin = stdinReader
		ptys = append(ptys, stdinReader)
		ttys = append(ttys, stdinWriter)
		go func() {
			<-wsutil.WebsocketRecvStream(stdinWriter, ioConn)
			stdinWriter.Close()
		}()

		// Start goroutine to receive from cmd.Stdout pipe and write to "io"
		// websocket.
		stdoutReader, stdoutWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		ptys = append(ptys, stdoutReader)
		ttys = append(ttys, stdoutWriter)
		stdout = stdoutWriter
		stderr = stdoutWriter // stderr will be overwritten below if splitStderr true
		wg.Add(1)
		go func() {
			<-wsutil.WebsocketSendStream(ioConn, stdoutReader, -1)
			stdoutReader.Close()
			wg.Done()
		}()
	}

	if e.splitStderr {
		// Start goroutine to receive from cmd.Stderr pipe and write to a
		// separate "stderr" websocket.
		stderrReader, stderrWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		ptys = append(ptys, stderrReader)
		ttys = append(ttys, stderrWriter)
		stderr = stderrWriter
		stderrConn := e.getWebsocket(wsStderr)
		wg.Add(1)
		go func() {
			<-wsutil.WebsocketSendStream(stderrConn, stderrReader, -1)
			stderrReader.Close()
			wg.Done()
		}()
	}

	if e.timeout != 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, e.command[0], e.command[1:]...)

	for k, v := range e.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Dir = e.workingDir

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if e.uid != nil && e.gid != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uint32(*e.uid),
			Gid: uint32(*e.gid),
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
	if e.useTerminal {
		cmd.SysProcAttr.Setctty = true
	}

	finisher := func(exitCode int, cmdErr error) error {
		for _, tty := range ttys {
			_ = tty.Close()
		}

		// Close the control channel, if connected.
		controlConn := e.getWebsocket(wsControl)
		if controlConn != nil {
			_ = controlConn.Close()
		}

		close(childDead)

		wg.Wait()

		for _, pty := range ptys {
			_ = pty.Close()
		}

		setApiData(change, exitCode)

		return cmdErr
	}

	// Start the command!
	err = cmd.Start()
	if err != nil {
		return finisher(-1, err)
	}

	// Send its PID to the control loop.
	pidCh <- cmd.Process.Pid

	// Wait for it to finish.
	err = cmd.Wait()

	// Handle errors: timeout, non-zero exit code, or other error.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return finisher(-1, fmt.Errorf("timed out after %v: %w", e.timeout, ctx.Err()))
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok {
			if status.Signaled() {
				// 128 + n == Fatal error signal "n"
				return finisher(128+int(status.Signal()), nil)
			}
			return finisher(status.ExitStatus(), nil)
		}
		return finisher(-1, err)
	} else if err != nil {
		return finisher(-1, err)
	}

	// Successful exit (exit code 0).
	return finisher(0, nil)
}

func setApiData(change *state.Change, exitCode int) {
	st := change.State()
	st.Lock()
	defer st.Unlock()
	change.Set("api-data", map[string]interface{}{
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

func (e *execution) controlLoop(pidCh <-chan int, exitCh <-chan struct{}, ptyFd int) {
	logger.Debugf("Control handler waiting")
	defer logger.Debugf("Control handler finished")

	// Wait till we receive the process's PID (command started).
	var pid int
	select {
	case pid = <-pidCh:
		break
	case <-exitCh:
		return
	}

	// Wait till the control websocket is connected.
	select {
	case <-e.controlConnected:
		break
	case <-exitCh:
		return
	}

	logger.Debugf("Control handler started for child PID %d", pid)
	for {
		controlConn := e.getWebsocket(wsControl)
		mt, r, err := controlConn.NextReader()
		if mt == websocket.CloseMessage {
			break
		}

		if err != nil {
			logger.Debugf("Error getting next reader for PID %d: %v", pid, err)
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
				logger.Noticef("Failed to send SIGKILL to pid %d", pid)
			} else {
				logger.Noticef("Sent SIGKILL to pid %d", pid)
			}
			break
		}

		var command execCommand
		err = json.NewDecoder(r).Decode(&command)
		if err != nil {
			logger.Noticef("Failed to unmarshal control socket command: %s", err)
			continue
		}

		switch {
		case command.Command == "resize" && e.useTerminal:
			if command.Resize == nil {
				logger.Noticef("Resize command requires width and height arguments")
				continue
			}
			w, h := command.Resize.Width, command.Resize.Height
			logger.Debugf("Received 'resize' command with size %dx%d", w, h)
			err = ptyutil.SetSize(ptyFd, w, h)
			if err != nil {
				logger.Noticef("Failed to set window size to: %dx%d", w, h)
				continue
			}
		case command.Command == "signal":
			if command.Signal == nil {
				logger.Noticef("Signal command requires signal name argument")
				continue
			}
			name := command.Signal.Name
			sig := unix.SignalNum(name)
			if sig == 0 {
				logger.Noticef("Invalid signal name %q", name)
				continue
			}
			logger.Debugf("Received 'signal' command with signal %s", name)
			err := unix.Kill(pid, sig)
			if err != nil {
				logger.Noticef("Failed forwarding %s to PID %d", name, pid)
				continue
			}
			logger.Noticef("Forwarded signal %s to PID %d", name, pid)
		default:
			logger.Noticef("Invalid command %q", command.Command)
		}
	}
}
