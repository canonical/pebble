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
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
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
	Terminal    bool
	Stderr      bool
	Width       int
	Height      int
}

// ExecMetadata is the metadata from an Exec call.
type ExecMetadata struct {
	WebsocketIDs map[string]string // keys are "control", "io", and "stderr" if Stderr true
	Environment  map[string]string
	WorkingDir   string
}

// Exec creates a task set that will execute the command with the given arguments.
func Exec(st *state.State, args *ExecArgs) (*state.Change, ExecMetadata, error) {
	env := map[string]string{}
	for k, v := range args.Environment {
		env[k] = v
	}

	// Set default value for PATH
	_, ok := env["PATH"]
	if !ok {
		env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	// If running as root, set some environment variable defaults
	if args.UserID != nil && *args.UserID == 0 {
		_, ok = env["HOME"]
		if !ok {
			env["HOME"] = "/root"
		}
		_, ok = env["USER"]
		if !ok {
			env["USER"] = "root"
		}
	}

	// Set default value for LANG
	_, ok = env["LANG"]
	if !ok {
		env["LANG"] = "C.UTF-8"
	}

	// Set default working directory to $HOME, or / if $HOME not set
	cwd := args.WorkingDir
	if cwd == "" {
		cwd = env["HOME"]
		if cwd == "" {
			cwd = "/"
		}
	}

	ws := &execWs{}

	ws.conns = map[string]*websocket.Conn{}
	ws.conns[wsControl] = nil
	ws.conns[wsIO] = nil
	if args.Stderr {
		ws.conns[wsStderr] = nil
	}
	ws.allConnected = make(chan bool, 1)
	ws.controlConnected = make(chan bool, 1)
	ws.terminal = args.Terminal
	ws.stderr = args.Stderr

	ws.wsIDs = map[string]string{}
	for key := range ws.conns {
		var err error
		ws.wsIDs[key], err = strutil.UUID()
		if err != nil {
			return nil, ExecMetadata{}, err
		}
	}

	ws.command = args.Command
	ws.env = env
	ws.timeout = args.Timeout

	ws.width = args.Width
	ws.height = args.Height

	ws.cwd = cwd
	ws.uid = args.UserID
	ws.gid = args.GroupID

	fds := make(map[string]string, len(ws.wsIDs))
	for key, id := range ws.wsIDs {
		fds[key] = id
	}
	metadata := ExecMetadata{
		WebsocketIDs: fds,
		Environment:  env,
		WorkingDir:   cwd,
	}

	// Create change object and store it in state
	cacheKey, err := strutil.UUID()
	if err != nil {
		return nil, ExecMetadata{}, err
	}
	st.Cache("exec-"+cacheKey, ws)
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

	ws, cacheKey, err := getWsAndCacheKey(change)
	if err != nil {
		return err
	}

	ctx := tomb.Context(context.Background())
	err = ws.do(ctx, change)

	deleteWsFromCache(change, cacheKey)

	return err
}

func getWsAndCacheKey(change *state.Change) (*execWs, string, error) {
	st := change.State()
	st.Lock()
	defer st.Unlock()

	var cacheKey string
	err := change.Get("cache-key", &cacheKey)
	if err != nil {
		return nil, "", err
	}

	ws := st.Cached("exec-" + cacheKey).(*execWs)
	return ws, cacheKey, nil
}

func deleteWsFromCache(change *state.Change, cacheKey string) {
	st := change.State()
	st.Lock()
	defer st.Unlock()
	st.Cache("exec-"+cacheKey, nil)
}

func Connect(change *state.Change, websocketID string, r *http.Request, w http.ResponseWriter) error {
	ws, _, err := getWsAndCacheKey(change)
	if err != nil {
		return err
	}
	return ws.connect(websocketID, r, w)
}

type execWs struct {
	command          []string
	env              map[string]string
	timeout          time.Duration
	conns            map[string]*websocket.Conn
	connsLock        sync.Mutex
	allConnected     chan bool
	controlConnected chan bool
	terminal         bool
	stderr           bool
	wsIDs            map[string]string
	width            int
	height           int
	uid              *int
	gid              *int
	cwd              string
}

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin:      func(r *http.Request) bool { return true },
	HandshakeTimeout: handshakeTimeout,
}

func (s *execWs) connect(id string, r *http.Request, w http.ResponseWriter) error {
	for key, wsID := range s.wsIDs {
		if id != wsID {
			continue
		}

		conn, err := websocketUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return err
		}

		s.connsLock.Lock()
		s.conns[key] = conn
		s.connsLock.Unlock()

		if key == wsControl {
			s.controlConnected <- true
			return nil
		}

		s.connsLock.Lock()
		for k, c := range s.conns {
			if k != wsControl && c == nil {
				s.connsLock.Unlock()
				return nil
			}
		}
		s.connsLock.Unlock()

		s.allConnected <- true
		return nil
	}

	return os.ErrNotExist
}

// waitAllConnected waits till all the websockets are connected or the connect
// timeout elapses (or the provided ctx is cancelled).
func (s *execWs) waitAllConnected(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Noticef("Timeout waiting for websocket connections: %v", ctx.Err())
			return fmt.Errorf("timeout waiting for websocket connections: %w", ctx.Err())
		}
		return ctx.Err()
	case <-s.allConnected:
		return nil
	}
}

func (s *execWs) do(ctx context.Context, change *state.Change) error {
	err := s.waitAllConnected(ctx)
	if err != nil {
		return err
	}

	var ttys []*os.File
	var ptys []*os.File

	var stdin *os.File
	var stdout *os.File
	var stderr *os.File

	controlExit := make(chan bool, 1)
	attachedChildIsBorn := make(chan int)
	attachedChildIsDead := make(chan struct{})
	var wgEOF sync.WaitGroup

	if s.terminal {
		ttys = make([]*os.File, 1)
		ptys = make([]*os.File, 1)

		var uid, gid int
		if s.uid != nil && s.gid != nil {
			uid, gid = *s.uid, *s.gid
		} else {
			uid = os.Getuid()
			gid = os.Getgid()
		}

		ptys[0], ttys[0], err = ptyutil.OpenPty(int64(uid), int64(gid))
		if err != nil {
			return err
		}

		stdin = ttys[0]
		stdout = ttys[0]
		stderr = ttys[0]

		if s.width > 0 && s.height > 0 {
			ptyutil.SetSize(int(ptys[0].Fd()), s.width, s.height)
		}

		go func() {
			logger.Debugf("Interactive child process handler waiting")
			defer logger.Debugf("Interactive child process handler finished")
			attachedChildPid := <-attachedChildIsBorn

			select {
			case <-s.controlConnected:
				break

			case <-controlExit:
				return
			}

			logger.Debugf("Interactive child process handler started for child PID %d", attachedChildPid)
			for {
				s.connsLock.Lock()
				conn := s.conns[wsControl]
				s.connsLock.Unlock()

				mt, r, err := conn.NextReader()
				if mt == websocket.CloseMessage {
					break
				}

				if err != nil {
					logger.Debugf("Got error getting next reader for child PID %d: %v", attachedChildPid, err)
					er, ok := err.(*websocket.CloseError)
					if !ok {
						break
					}

					if er.Code != websocket.CloseAbnormalClosure {
						break
					}

					// If an abnormal closure occurred, kill the attached process.
					err := unix.Kill(attachedChildPid, unix.SIGKILL)
					if err != nil {
						logger.Noticef("Failed to send SIGKILL to pid %d", attachedChildPid)
					} else {
						logger.Noticef("Sent SIGKILL to pid %d", attachedChildPid)
					}
					return
				}

				buf, err := ioutil.ReadAll(r)
				if err != nil {
					logger.Noticef("Failed to read message %s", err)
					break
				}

				var command struct {
					Command string            `json:"command"`
					Args    map[string]string `json:"args"`
					Signal  int               `json:"signal"`
				}
				if err := json.Unmarshal(buf, &command); err != nil {
					logger.Noticef("Failed to unmarshal control socket command: %s", err)
					continue
				}

				if command.Command == "window-resize" {
					winchWidth, err := strconv.Atoi(command.Args["width"])
					if err != nil {
						logger.Noticef("Unable to extract window width: %s", err)
						continue
					}

					winchHeight, err := strconv.Atoi(command.Args["height"])
					if err != nil {
						logger.Noticef("Unable to extract window height: %s", err)
						continue
					}

					ptyutil.SetSize(int(ptys[0].Fd()), winchWidth, winchHeight)
					if err != nil {
						logger.Noticef("Failed to set window size to: %dx%d", winchWidth, winchHeight)
						continue
					}
				} else if command.Command == "signal" {
					if err := unix.Kill(attachedChildPid, unix.Signal(command.Signal)); err != nil {
						logger.Noticef("Failed forwarding signal '%d' to PID %d", command.Signal, attachedChildPid)
						continue
					}
					logger.Noticef("Forwarded signal '%d' to PID %d", command.Signal, attachedChildPid)
				}
			}
		}()

		wgEOF.Add(1)
		go func() {
			s.connsLock.Lock()
			conn := s.conns[wsIO]
			s.connsLock.Unlock()

			logger.Debugf("Started mirroring websocket")
			readDone, writeDone := wsutil.WebsocketExecMirror(conn, ptys[0], ptys[0], attachedChildIsDead, int(ptys[0].Fd()))

			<-readDone
			<-writeDone
			logger.Debugf("Finished mirroring websocket")

			conn.Close()
			wgEOF.Done()
		}()
	} else {
		// TODO: need to run control handler in !Terminal mode too (signals only)

		// Receive stdin from "io" websocket, write to cmd.Stdin pipe
		s.connsLock.Lock()
		ioConn := s.conns[wsIO]
		s.connsLock.Unlock()
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

		// Receive from cmd.Stdout pipe, write to "io" websocket
		stdoutReader, stdoutWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		ptys = append(ptys, stdoutReader)
		ttys = append(ttys, stdoutWriter)
		stdout = stdoutWriter
		wgEOF.Add(1)
		go func() {
			<-wsutil.WebsocketSendStream(ioConn, stdoutReader, -1)
			stdoutReader.Close()
			wgEOF.Done()
		}()

		// Receive from cmd.Stderr pipe, write to "io" websocket as well (or
		// "stderr" websocket if client wants stderr separate).
		stderrReader, stderrWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		ptys = append(ptys, stderrReader)
		ttys = append(ttys, stderrWriter)
		stderr = stderrWriter
		stderrConn := ioConn // TODO(benhoyt): this won't work as is -- websocket.Conn writes aren't concurrency safe
		if s.stderr {
			s.connsLock.Lock()
			stderrConn = s.conns[wsStderr]
			s.connsLock.Unlock()
		}
		wgEOF.Add(1)
		go func() {
			<-wsutil.WebsocketSendStream(stderrConn, stderrReader, -1)
			stderrReader.Close()
			wgEOF.Done()
		}()
	}

	finisher := func(cmdResult int, cmdErr error) error {
		for _, tty := range ttys {
			tty.Close()
		}

		s.connsLock.Lock()
		conn := s.conns[wsControl]
		s.connsLock.Unlock()

		if conn == nil {
			if s.terminal {
				controlExit <- true
			}
		} else {
			conn.Close()
		}

		close(attachedChildIsDead)

		wgEOF.Wait()

		for _, pty := range ptys {
			pty.Close()
		}

		setApiData(change, cmdResult)

		return cmdErr
	}

	if s.timeout != 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, s.command[0], s.command[1:]...)

	// Prepare the environment
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if s.uid != nil && s.gid != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uint32(*s.uid),
			Gid: uint32(*s.gid),
		}
	}

	// Creates a new session if the calling process is not a process group leader.
	// The calling process is the leader of the new session, the process group leader of
	// the new process group, and has no controlling terminal.
	// This is important to allow remote shells to handle ctrl+c.
	cmd.SysProcAttr.Setsid = true

	// Make the given terminal the controlling terminal of the calling process.
	// The calling process must be a session leader and not have a controlling terminal already.
	// This is important as allows ctrl+c to work as expected for non-shell programs.
	if s.terminal {
		cmd.SysProcAttr.Setctty = true
	}

	cmd.Dir = s.cwd

	err = cmd.Start()
	if err != nil {
		return finisher(-1, err)
	}

	if s.terminal {
		attachedChildIsBorn <- cmd.Process.Pid
	}

	err = cmd.Wait()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return finisher(-1, fmt.Errorf("timed out after %v: %w", s.timeout, ctx.Err()))
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok {
			return finisher(status.ExitStatus(), nil)
		}
		if status.Signaled() {
			// 128 + n == Fatal error signal "n"
			return finisher(128+int(status.Signal()), nil)
		}
		return finisher(-1, err)
	} else if err != nil {
		return finisher(-1, err)
	}

	return finisher(0, nil)
}

func setApiData(change *state.Change, cmdResult int) {
	st := change.State()
	st.Lock()
	defer st.Unlock()
	change.Set("api-data", map[string]interface{}{
		"return": cmdResult,
	})
}
