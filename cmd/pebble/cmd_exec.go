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
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/ptyutil"
)

type cmdExec struct {
	clientMixin
	WorkingDir string        `short:"w"`
	Env        []string      `long:"env"`
	UserID     *int          `long:"uid"`
	User       string        `long:"user"`
	GroupID    *int          `long:"gid"`
	Group      string        `long:"group"`
	Timeout    time.Duration `long:"timeout"`
	Terminal   bool          `short:"t"`
	NoTerminal bool          `short:"T"`
	Positional struct {
		Command string `positional-arg-name:"<command>" required:"1"`
	} `positional-args:"yes"`
}

var execDescs = map[string]string{
	"w":       "Working directory to run command in",
	"env":     "Environment variable to set (in 'FOO=bar' format)",
	"uid":     "User ID to run command as",
	"user":    "Username to run command as (user's UID must match uid if both present)",
	"gid":     "Group ID to run command as",
	"group":   "Group name to run command as (group's GID must match gid if both present)",
	"timeout": "Timeout after which to terminate command",
	"t":       "Allocate remote pseudo-terminal (default if stdin and stdout are TTYs)",
	"T":       "Disable remote pseudo-terminal allocation",
}

var shortExecHelp = "Execute a remote command and wait for it to finish"
var longExecHelp = `
The exec command runs a remote command and waits for it to finish. The local
stdin is sent as the input to the remote process, while the remote stdout and
stderr are output locally.

To avoid confusion, exec options may be separated from the command and its
arguments using "--", for example:

pebble exec --timeout 10s -- echo -n foo bar
`

func (cmd *cmdExec) Execute(args []string) error {
	if cmd.Terminal && cmd.NoTerminal {
		return errors.New("cannot use -t and -T at the same time")
	}

	command := append([]string{cmd.Positional.Command}, args...)
	logger.Debugf("Executing command %q", command)

	// Set up environment variables.
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

	// Specify UseTerminal if -t/--terminal is given, or if both stdin and
	// stdout are TTYs.
	stdinIsTerminal := ptyutil.IsTerminal(unix.Stdin)
	stdoutIsTerminal := ptyutil.IsTerminal(unix.Stdout)
	var useTerminal bool
	if cmd.Terminal {
		useTerminal = true
	} else if cmd.NoTerminal {
		useTerminal = false
	} else {
		useTerminal = stdinIsTerminal && stdoutIsTerminal
	}

	// Record terminal state (and restore it before we exit).
	if useTerminal && stdinIsTerminal {
		oldState, err := ptyutil.MakeRaw(unix.Stdin)
		if err != nil {
			return err
		}
		defer ptyutil.Restore(unix.Stdin, oldState)
	}

	// Grab current terminal dimensions.
	var width, height int
	if stdoutIsTerminal {
		var err error
		width, height, err = ptyutil.GetSize(unix.Stdout)
		if err != nil {
			return err
		}
	}

	opts := &client.ExecOptions{
		Command:     command,
		Environment: env,
		WorkingDir:  cmd.WorkingDir,
		Timeout:     cmd.Timeout,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
		UseTerminal: useTerminal,
		Width:       width,
		Height:      height,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	// If stdout and stderr both refer to the same file or device (e.g.,
	// "/dev/pts/1"), combine stderr into stdout on the server.
	stdoutPath, err := os.Readlink("/proc/self/fd/1")
	if err == nil {
		stderrPath, err := os.Readlink("/proc/self/fd/2")
		if err == nil && stdoutPath == stderrPath {
			opts.Stderr = nil // opts.Stderr nil uses "combine stderr" mode
		}
	}

	// Start the command.
	process, err := cmd.client.Exec(opts)
	if err != nil {
		return err
	}

	// Start a goroutine to handle signals and window resizing.
	done := make(chan struct{})
	defer close(done)
	go execControlHandler(process, useTerminal, done)

	// Wait for the command to finish.
	err = process.Wait()
	switch e := err.(type) {
	case nil:
		return nil
	case *client.ExitError:
		logger.Debugf("Process exited with return code %d", e.ExitCode())
		panic(&exitStatus{e.ExitCode()})
	default:
		return err
	}
}

func execControlHandler(process *client.ExecProcess, useTerminal bool, done <-chan struct{}) {
	ch := make(chan os.Signal, 10)
	signal.Notify(ch,
		unix.SIGWINCH, unix.SIGHUP,
		unix.SIGTERM, unix.SIGINT, unix.SIGQUIT, unix.SIGABRT,
		unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU, unix.SIGUSR1,
		unix.SIGUSR2, unix.SIGSEGV, unix.SIGCONT)

	for {
		var sig os.Signal
		select {
		case sig = <-ch:
		case <-done:
			return
		}

		switch sig {
		case unix.SIGWINCH:
			if !useTerminal {
				logger.Debugf("Received SIGWINCH but not in terminal mode, ignoring")
				break
			}
			logger.Debugf("Received '%s signal', updating window geometry", sig)
			width, height, err := ptyutil.GetSize(unix.Stdout)
			if err != nil {
				logger.Debugf("Cannot get terminal size: %v", err)
				break
			}
			logger.Debugf("Window size is now: %dx%d", width, height)
			err = process.SendResize(width, height)
			if err != nil {
				logger.Debugf("Cannot set terminal size: %v", err)
				break
			}
		case unix.SIGHUP, unix.SIGTERM, unix.SIGINT, unix.SIGQUIT,
			unix.SIGABRT, unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU,
			unix.SIGUSR1, unix.SIGUSR2, unix.SIGSEGV, unix.SIGCONT:
			logger.Debugf("Received '%s' signal, forwarding to executing program", sig)
			err := process.SendSignal(unix.SignalName(sig.(unix.Signal)))
			if err != nil {
				logger.Debugf("Cannot forward signal '%s': %v", sig, err)
				break
			}
		}
	}
}

func init() {
	addCommand("exec", shortExecHelp, longExecHelp, func() flags.Commander { return &cmdExec{} }, execDescs, nil)
}
