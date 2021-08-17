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

package main

import (
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jessevdk/go-flags"
	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/ptyutil"
)

type cmdExec struct {
	clientMixin
	WorkingDir          string        `long:"cwd"`
	DisableStdin        bool          `short:"n" long:"disable-stdin"`
	Env                 []string      `long:"env"`
	User                string        `long:"user"`
	Group               string        `long:"group"`
	Timeout             time.Duration `long:"timeout"`
	ForceInteractive    bool          `short:"t" long:"force-interactive"`
	ForceNonInteractive bool          `short:"T" long:"force-noninteractive"`
	Positional          struct {
		Command string `positional-arg-name:"<command>" required:"1"`
	} `positional-args:"yes"`
}

var execDescs = map[string]string{
	"cwd":                  "Working directory to run command in",
	"disable-stdin":        "Disable stdin (reads from /dev/null)",
	"env":                  "Environment variable to set (in 'FOO=bar' format)",
	"user":                 "User name or ID to run command as",
	"group":                "Group name or ID to run command as",
	"timeout":              "Timeout after which to terminate command",
	"force-interactive":    "Force pseudo-terminal allocation",
	"force-noninteractive": "Disable pseudo-terminal allocation",
}

var shortExecHelp = "Execute a command"
var longExecHelp = `
The exec command executes a command via the Pebble API and waits for it to
finish. Stdout and stderr are forwarded, and stdin is forwarded unless
-n/--disable-stdin is specified. By default, interactive mode is used if the
terminal is a TTY, meaning signals and window resizing are forwarded (use
-t/--force-interactive or -T/--force-noninteractive to override).

To avoid confusion, exec options may be separated from the command and its
arguments using "--", for example:

pebble exec --timeout 10s -- echo foo bar
`

func (cmd *cmdExec) Execute(args []string) error {
	if cmd.ForceInteractive && cmd.ForceNonInteractive {
		return errors.New("can't pass -t and -T at the same time")
	}

	command := append([]string{cmd.Positional.Command}, args...)
	logger.Debugf("Executing command %q", command)

	// Set up any environment variables
	env := make(map[string]string)
	for _, kv := range cmd.Env {
		parts := strings.SplitN(kv, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		env[key] = value
	}

	// Send UID if it looks like an integer, otherwise send user name.
	var user string
	var userID *int
	uid, err := strconv.Atoi(cmd.User)
	if err != nil {
		user = cmd.User
	} else {
		userID = &uid
	}

	// Send GID if it looks like an integer, otherwise send group name.
	var group string
	var groupID *int
	gid, err := strconv.Atoi(cmd.Group)
	if err != nil {
		group = cmd.Group
	} else {
		groupID = &gid
	}

	// Determine interaction mode
	stdinTerminal := ptyutil.IsTerminal(unix.Stdin)
	stdoutTerminal := ptyutil.IsTerminal(unix.Stdout)
	var interactive bool
	if cmd.ForceInteractive {
		interactive = true
	} else if cmd.ForceNonInteractive {
		interactive = false
	} else {
		interactive = stdinTerminal && stdoutTerminal
	}

	// Record terminal state
	if interactive && stdinTerminal {
		oldState, err := ptyutil.MakeRaw(unix.Stdin)
		if err != nil {
			return err
		}
		defer ptyutil.Restore(unix.Stdin, oldState)
	}

	// Grab current terminal dimensions
	var width, height int
	if stdoutTerminal {
		width, height, err = ptyutil.GetSize(unix.Stdout)
		if err != nil {
			return err
		}
	}

	// Run the command
	opts := &client.ExecOptions{
		Mode:        client.ExecStreaming,
		Command:     command,
		Environment: env,
		WorkingDir:  cmd.WorkingDir,
		Timeout:     cmd.Timeout,
		User:        user,
		UserID:      userID,
		Group:       group,
		GroupID:     groupID,
		Width:       width,
		Height:      height,
	}
	additionalArgs := &client.ExecAdditionalArgs{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		DataDone: make(chan bool),
	}
	if interactive {
		opts.Mode = client.ExecInteractive
		additionalArgs.Control = execControlHandler
	}
	changeID, err := cmd.client.Exec(opts, additionalArgs)
	if err != nil {
		return err
	}

	// Wait till the command (change) is finished
	waitOpts := &client.WaitChangeOptions{}
	if cmd.Timeout != 0 {
		// A little more than the command timeout to ensure that happens first
		waitOpts.Timeout = cmd.Timeout + time.Second
	}
	change, err := cmd.client.WaitChange(changeID, waitOpts)
	if err != nil {
		return err
	}
	if !change.Ready {
		// This case shouldn't happen (because the change should either be ready,
		// or if a timeout was specified it will apply at the Exec level and the
		// change will be Ready but in an error state), but handle just in case.
		return errors.New("command unexpectedly not finished after waiting for change")
	}
	if change.Err != "" {
		return errors.New(change.Err)
	}
	var returnCode int
	err = change.Get("return", &returnCode)
	if err != nil {
		return err
	}
	if returnCode != 0 {
		logger.Debugf("Process exited with return code %d", returnCode)
		panic(&exitStatus{returnCode})
	}

	// Wait for any remaining I/O to be flushed
	<-additionalArgs.DataDone

	return nil
}

func execControlHandler(control *websocket.Conn) {
	ch := make(chan os.Signal, 10)
	signal.Notify(ch,
		unix.SIGWINCH,
		unix.SIGTERM,
		unix.SIGHUP,
		unix.SIGINT,
		unix.SIGQUIT,
		unix.SIGABRT,
		unix.SIGTSTP,
		unix.SIGTTIN,
		unix.SIGTTOU,
		unix.SIGUSR1,
		unix.SIGUSR2,
		unix.SIGSEGV,
		unix.SIGCONT)

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	defer control.WriteMessage(websocket.CloseMessage, closeMsg)

	// TODO: can we combine the forward-signal cases?
	for {
		sig := <-ch
		switch sig {
		case unix.SIGWINCH:
			logger.Debugf("Received '%s signal', updating window geometry.", sig)
			err := sendTermSize(control)
			if err != nil {
				logger.Debugf("error setting term size %s", err)
				return
			}
		case unix.SIGTERM:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGTERM)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGTERM)
				return
			}
		case unix.SIGHUP:
			file, err := os.OpenFile("/dev/tty", os.O_RDONLY|unix.O_NOCTTY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0666)
			if err == nil {
				file.Close()
				err = forwardSignal(control, unix.SIGHUP)
			} else {
				err = forwardSignal(control, unix.SIGTERM)
				sig = unix.SIGTERM
			}
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", sig)
				return
			}
		case unix.SIGINT:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGINT)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGINT)
				return
			}
		case unix.SIGQUIT:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGQUIT)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGQUIT)
				return
			}
		case unix.SIGABRT:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGABRT)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGABRT)
				return
			}
		case unix.SIGTSTP:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGTSTP)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGTSTP)
				return
			}
		case unix.SIGTTIN:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGTTIN)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGTTIN)
				return
			}
		case unix.SIGTTOU:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGTTOU)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGTTOU)
				return
			}
		case unix.SIGUSR1:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGUSR1)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGUSR1)
				return
			}
		case unix.SIGUSR2:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGUSR2)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGUSR2)
				return
			}
		case unix.SIGSEGV:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGSEGV)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGSEGV)
				return
			}
		case unix.SIGCONT:
			logger.Debugf("Received '%s signal', forwarding to executing program.", sig)
			err := forwardSignal(control, unix.SIGCONT)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s'.", unix.SIGCONT)
				return
			}
		}
	}
}

func sendTermSize(control *websocket.Conn) error {
	width, height, err := ptyutil.GetSize(unix.Stdout)
	if err != nil {
		return err
	}
	logger.Debugf("Window size is now: %dx%d", width, height)
	return client.ExecSendTermSize(control, width, height)
}

func forwardSignal(control *websocket.Conn, sig unix.Signal) error {
	logger.Debugf("Forwarding signal: %s", sig)
	return client.ExecForwardSignal(control, int(sig))
}

func init() {
	addCommand("exec", shortExecHelp, longExecHelp, func() flags.Commander { return &cmdExec{} }, execDescs, nil)
}
