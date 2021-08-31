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
	WorkingDir string        `long:"cwd"`
	Env        []string      `long:"env"`
	User       string        `long:"user"`
	Group      string        `long:"group"`
	Timeout    time.Duration `long:"timeout"`
	Terminal   bool          `short:"t" long:"terminal"`
	NoTerminal bool          `short:"T" long:"no-terminal"`
	Positional struct {
		Command string `positional-arg-name:"<command>" required:"1"`
	} `positional-args:"yes"`
}

var execDescs = map[string]string{
	"cwd":         "Working directory to run command in",
	"env":         "Environment variable to set (in 'FOO=bar' format)",
	"user":        "User name or ID to run command as",
	"group":       "Group name or ID to run command as",
	"timeout":     "Timeout after which to terminate command",
	"terminal":    "Force pseudo-terminal allocation",
	"no-terminal": "Disable pseudo-terminal allocation",
}

var shortExecHelp = "Execute a command"
var longExecHelp = `
The exec command executes a command via the Pebble API and waits for it to
finish. Stdin is forwarded, and stdout and stderr are received. By default,
exec's terminal mode is used if the terminal is a TTY (use -t/--terminal or
-T/--no-terminal to override).

To avoid confusion, exec options may be separated from the command and its
arguments using "--", for example:

pebble exec --timeout 10s -- echo -n foo bar
`

func (cmd *cmdExec) Execute(args []string) error {
	if cmd.Terminal && cmd.NoTerminal {
		return errors.New("can't pass -t and -T at the same time")
	}

	command := append([]string{cmd.Positional.Command}, args...)
	logger.Debugf("Executing command %q", command)

	// Set up environment variables
	env := make(map[string]string)
	term, ok := os.LookupEnv("TERM")
	if ok {
		env["TERM"] = term
	}
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
	var terminal bool
	if cmd.Terminal {
		terminal = true
	} else if cmd.NoTerminal {
		terminal = false
	} else {
		terminal = stdinTerminal && stdoutTerminal
	}

	// Record terminal state (and restore it later)
	if terminal && stdinTerminal {
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
		Command:     command,
		Environment: env,
		WorkingDir:  cmd.WorkingDir,
		Timeout:     cmd.Timeout,
		User:        user,
		UserID:      userID,
		Group:       group,
		GroupID:     groupID,
		Terminal:    terminal,
		Stderr:      !terminal,
		Width:       width,
		Height:      height,
	}
	additionalArgs := &client.ExecAdditionalArgs{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Control: func(conn client.WebsocketWriter) {
			execControlHandler(conn, terminal)
		},
		DataDone: make(chan bool),
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
	if change.Err != "" {
		return errors.New(change.Err)
	}
	var exitCode int
	err = change.Get("return", &exitCode)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		logger.Debugf("Process exited with return code %d", exitCode)
		return &exitStatus{exitCode}
	}

	// Wait for any remaining I/O to be flushed
	<-additionalArgs.DataDone

	return nil
}

func execControlHandler(control client.WebsocketWriter, terminal bool) {
	ch := make(chan os.Signal, 10)
	signal.Notify(ch,
		unix.SIGWINCH, unix.SIGHUP,
		unix.SIGTERM, unix.SIGINT, unix.SIGQUIT, unix.SIGABRT,
		unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU, unix.SIGUSR1,
		unix.SIGUSR2, unix.SIGSEGV, unix.SIGCONT)

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	defer control.WriteMessage(websocket.CloseMessage, closeMsg)

	for {
		sig := <-ch
		switch sig {
		case unix.SIGWINCH:
			if !terminal {
				logger.Debugf("Received SIGWINCH but not in terminal mode, ignoring")
				break
			}
			logger.Debugf("Received '%s signal', updating window geometry", sig)
			width, height, err := ptyutil.GetSize(unix.Stdout)
			if err != nil {
				logger.Debugf("Error getting terminal size: %v", err)
				break
			}
			logger.Debugf("Window size is now: %dx%d", width, height)
			err = client.ExecSendTermSize(control, width, height)
			if err != nil {
				logger.Debugf("Error setting terminal size: %v", err)
				break
			}
		case unix.SIGHUP:
			file, err := os.OpenFile("/dev/tty", os.O_RDONLY|unix.O_NOCTTY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0666)
			if err == nil {
				file.Close()
				err = client.ExecForwardSignal(control, int(unix.SIGHUP))
			} else {
				err = client.ExecForwardSignal(control, int(unix.SIGTERM))
				sig = unix.SIGTERM
			}
			logger.Debugf("Received '%s' signal, forwarding to executing program", sig)
			if err != nil {
				logger.Debugf("Failed to forward signal '%s': %v", sig, err)
				return
			}
		case unix.SIGTERM, unix.SIGINT, unix.SIGQUIT, unix.SIGABRT,
			unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU, unix.SIGUSR1,
			unix.SIGUSR2, unix.SIGSEGV, unix.SIGCONT:
			logger.Debugf("Received '%s signal', forwarding to executing program", sig)
			err := client.ExecForwardSignal(control, int(sig.(unix.Signal)))
			if err != nil {
				logger.Debugf("Failed to forward signal '%s': %v", sig, err)
				break
			}
		}
	}
}

func init() {
	addCommand("exec", shortExecHelp, longExecHelp, func() flags.Commander { return &cmdExec{} }, execDescs, nil)
}
