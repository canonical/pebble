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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/strutil"
	"github.com/canonical/pebble/internal/wsutil"
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
	UserID      *int
	GroupID     *int
	Interactive bool
}

// Exec creates a task set that will execute the command with the given arguments.
func Exec(st *state.State, args *ExecArgs) (*state.TaskSet, map[string]interface{}, error) {
	cacheKey, err := strutil.UUID()
	if err != nil {
		return nil, nil, err
	}

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
	ws.fds = map[int]string{}

	ws.conns = map[int]*websocket.Conn{}
	ws.conns[-1] = nil
	ws.conns[0] = nil
	if !args.Interactive {
		ws.conns[1] = nil
		ws.conns[2] = nil
	}
	ws.allConnected = make(chan bool, 1)
	ws.controlConnected = make(chan bool, 1)
	ws.interactive = args.Interactive
	for i := -1; i < len(ws.conns)-1; i++ {
		ws.fds[i], err = strutil.UUID()
		if err != nil {
			return nil, nil, err
		}
	}

	ws.command = args.Command
	ws.env = env

	// TODO: interactive
	//ws.width = post.Width
	//ws.height = post.Height

	ws.cwd = cwd
	// TODO: uid and gid handling
	//ws.uid = args.UserID
	//TODO ws.gid = args.GroupID

	logger.Noticef("ERROR: cmdstate.Exec ws=%+v", ws)
	st.Cache("exec-"+cacheKey, ws)

	summary := fmt.Sprintf("exec command %q", args.Command[0])
	task := st.NewTask("exec", summary)
	task.Set("cache-key", cacheKey)

	fds := make(map[string]string)
	for fd, secret := range ws.fds {
		if fd == -1 {
			fds["control"] = secret
		} else {
			fds[strconv.Itoa(fd)] = secret
		}
	}
	metadata := map[string]interface{}{
		"environment": env,
		"fds":         fds,
		"working-dir": cwd,
	}

	return state.NewTaskSet(task), metadata, nil
}

// TODO: use tomb to cancel?
func doExec(task *state.Task, tomb *tomb.Tomb) error {
	var cacheKey string
	err := task.Get("cache-key", &cacheKey)
	if err != nil {
		return err
	}

	st := task.State()
	st.Lock()
	ws := st.Cached("exec-" + cacheKey).(*execWs)
	change := task.Change()
	st.Unlock()

	logger.Noticef("TODO doExec start: %+v", ws)

	err = ws.Do(st, change)
	return err
}

func Connect(st *state.State, cacheKey string, r *http.Request, w http.ResponseWriter) error {
	st.Lock()
	ws := st.Cached("exec-" + cacheKey).(*execWs)
	st.Unlock()
	return ws.Connect(r, w)
}

type execWs struct {
	command          []string
	env              map[string]string
	conns            map[int]*websocket.Conn
	connsLock        sync.Mutex
	allConnected     chan bool
	controlConnected chan bool
	interactive      bool
	fds              map[int]string
	width            int
	height           int
	uid              uint32
	gid              uint32
	cwd              string
}

func (s *execWs) Connect(r *http.Request, w http.ResponseWriter) error {
	secret := r.FormValue("secret")
	logger.Noticef("TODO execWs.Connect secret=%s", secret)
	if secret == "" {
		return fmt.Errorf("missing secret")
	}

	for fd, fdSecret := range s.fds {
		logger.Noticef("TODO: execWs.Connect fd=%d", fd)
		if secret == fdSecret {
			logger.Noticef("TODO: execWs.Connect fd=%d, fdSecret=%s", fd, fdSecret)
			conn, err := wsutil.WebsocketUpgrader.Upgrade(w, r, nil)
			if err != nil {
				logger.Errorf("TODO: execWs.Connect upgrade error: %v", err)
				return err
			}

			s.connsLock.Lock()
			s.conns[fd] = conn
			s.connsLock.Unlock()

			if fd == -1 {
				logger.Noticef("TODO: execWs.Connect control connected")
				s.controlConnected <- true
				return nil
			}

			s.connsLock.Lock()
			for i, c := range s.conns {
				if i != -1 && c == nil {
					s.connsLock.Unlock()
					logger.Noticef("TODO: execWs.Connect connected (not yet all)")
					return nil
				}
			}
			s.connsLock.Unlock()

			logger.Noticef("TODO: execWs.Connect all connected")
			s.allConnected <- true
			return nil
		}
	}

	/* If we didn't find the right secret, the user provided a bad one,
	 * which 403, not 404, since this Operation actually exists */
	return os.ErrPermission
}

func (s *execWs) Do(st *state.State, change *state.Change) error {
	logger.Infof("TODO execWs.Do before allConnected")
	// TODO: shouldn't this have some kind of connect timeout?
	<-s.allConnected
	logger.Infof("TODO execWs.Do after allConnected")

	var err error
	var ttys []*os.File
	var ptys []*os.File

	var stdin *os.File
	var stdout *os.File
	var stderr *os.File

	if s.interactive {
		ttys = make([]*os.File, 1)
		ptys = make([]*os.File, 1)
		//TODO		ptys[0], ttys[0], err = shared.OpenPty(int64(s.uid), int64(s.gid))
		if err != nil {
			return err
		}

		stdin = ttys[0]
		stdout = ttys[0]
		stderr = ttys[0]

		if s.width > 0 && s.height > 0 {
			//TODO			shared.SetSize(int(ptys[0].Fd()), s.width, s.height)
		}
	} else {
		ttys = make([]*os.File, 3)
		ptys = make([]*os.File, 3)
		for i := 0; i < len(ttys); i++ {
			ptys[i], ttys[i], err = os.Pipe()
			if err != nil {
				return err
			}
		}

		stdin = ptys[0]
		stdout = ttys[1]
		stderr = ttys[2]
	}

	controlExit := make(chan bool, 1)
	attachedChildIsBorn := make(chan int)
	attachedChildIsDead := make(chan struct{})
	var wgEOF sync.WaitGroup

	if s.interactive {
		wgEOF.Add(1)
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
				conn := s.conns[-1]
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
						logger.Errorf("Failed to send SIGKILL to pid %d", attachedChildPid)
					} else {
						logger.Infof("Sent SIGKILL to pid %d", attachedChildPid)
					}
					return
				}

				buf, err := ioutil.ReadAll(r)
				if err != nil {
					logger.Errorf("Failed to read message %s", err)
					break
				}

				var command struct {
					Command string            `json:"command"`
					Args    map[string]string `json:"args"`
					Signal  int               `json:"signal"`
				}

				if err := json.Unmarshal(buf, &command); err != nil {
					logger.Errorf("Failed to unmarshal control socket command: %s", err)
					continue
				}

				if command.Command == "window-resize" {
					winchWidth, err := strconv.Atoi(command.Args["width"])
					if err != nil {
						logger.Errorf("Unable to extract window width: %s", err)
						continue
					}

					winchHeight, err := strconv.Atoi(command.Args["height"])
					if err != nil {
						logger.Errorf("Unable to extract window height: %s", err)
						continue
					}

					//TODO: err = shared.SetSize(int(ptys[0].Fd()), winchWidth, winchHeight)
					if err != nil {
						logger.Errorf("Failed to set window size to: %dx%d", winchWidth, winchHeight)
						continue
					}
				} else if command.Command == "signal" {
					if err := unix.Kill(attachedChildPid, unix.Signal(command.Signal)); err != nil {
						logger.Errorf("Failed forwarding signal '%d' to PID %d", command.Signal, attachedChildPid)
						continue
					}
					logger.Infof("Forwarded signal '%d' to PID %d", command.Signal, attachedChildPid)
				}
			}
		}()

		go func() {
			s.connsLock.Lock()
			conn := s.conns[0]
			s.connsLock.Unlock()

			logger.Infof("Started mirroring websocket")
			readDone, writeDone := wsutil.WebsocketExecMirror(conn, ptys[0], ptys[0], attachedChildIsDead, int(ptys[0].Fd()))

			<-readDone
			<-writeDone
			logger.Infof("Finished mirroring websocket")

			conn.Close()
			wgEOF.Done()
		}()
	} else {
		wgEOF.Add(len(ttys) - 1)
		for i := 0; i < len(ttys); i++ {
			go func(i int) {
				if i == 0 {
					s.connsLock.Lock()
					conn := s.conns[i]
					s.connsLock.Unlock()

					<-wsutil.WebsocketRecvStream(ttys[i], conn)
					ttys[i].Close()
				} else {
					s.connsLock.Lock()
					conn := s.conns[i]
					s.connsLock.Unlock()

					<-wsutil.WebsocketSendStream(conn, ptys[i], -1)
					ptys[i].Close()
					wgEOF.Done()
				}
			}(i)
		}
	}

	finisher := func(cmdResult int, cmdErr error) error {
		logger.Noticef("TODO finisher: cmdResult=%d, cmdErr=%v", cmdResult, cmdErr)
		for _, tty := range ttys {
			tty.Close()
		}

		s.connsLock.Lock()
		conn := s.conns[-1]
		s.connsLock.Unlock()

		if conn == nil {
			if s.interactive {
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

		st.Lock()
		change.Set("api-data", map[string]interface{}{
			"return": cmdResult,
		})
		st.Unlock()

		return cmdErr
	}

	var cmd *exec.Cmd

	if len(s.command) > 1 {
		cmd = exec.Command(s.command[0], s.command[1:]...)
	} else {
		cmd = exec.Command(s.command[0])
	}

	// Prepare the environment
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		//TODO: Credential: &syscall.Credential{
		//	Uid: s.uid,
		//	Gid: s.gid,
		//},
		// Creates a new session if the calling process is not a process group leader.
		// The calling process is the leader of the new session, the process group leader of
		// the new process group, and has no controlling terminal.
		// This is important to allow remote shells to handle ctrl+c.
		Setsid: true,
	}

	// Make the given terminal the controlling terminal of the calling process.
	// The calling process must be a session leader and not have a controlling terminal already.
	// This is important as allows ctrl+c to work as expected for non-shell programs.
	if s.interactive {
		cmd.SysProcAttr.Setctty = true
	}

	cmd.Dir = s.cwd

	logger.Noticef("TODO: starting command, args=%q", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return finisher(-1, err)
	}

	if s.interactive {
		attachedChildIsBorn <- cmd.Process.Pid
	}

	logger.Noticef("TODO: waiting for command...")
	err = cmd.Wait()
	if err == nil {
		return finisher(0, nil)
	}

	exitErr, ok := err.(*exec.ExitError)
	if ok {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok {
			return finisher(status.ExitStatus(), nil)
		}

		if status.Signaled() {
			// 128 + n == Fatal error signal "n"
			return finisher(128+int(status.Signal()), nil)
		}
	}

	return finisher(-1, nil)
}
