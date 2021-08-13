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
	"strconv"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/logger"
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
	command := append([]string{cmd.Positional.Command}, args...)
	logger.Debugf("Executing command %q", command)

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

	opts := &client.ExecOptions{
		Command:     command,
		Environment: env,
		WorkingDir:  cmd.WorkingDir,
		Timeout:     cmd.Timeout,
		User:        user,
		UserID:      userID,
		Group:       group,
		GroupID:     groupID,
	}
	additionalArgs := &client.ExecAdditionalArgs{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Control:  nil, // TODO: implement control handler (from LXD)
		DataDone: make(chan bool),
	}
	changeID, err := cmd.client.Exec(opts, additionalArgs)
	if err != nil {
		return err
	}

	// TODO: add /v1/changes/{id}/wait and use that instead
	var returnCode int
	var returnErr error
	for {
		ch, err := cmd.client.Change(changeID)
		if err != nil {
			return err
		}
		if ch.Ready {
			if ch.Err != "" {
				returnErr = errors.New(ch.Err)
				break
			}
			err := ch.Get("return", &returnCode)
			if err != nil {
				return err
			}
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	// Wait for any remaining I/O to be flushed
	<-additionalArgs.DataDone

	if returnCode != 0 {
		logger.Debugf("Process exited with return code %d", returnCode)
		panic(&exitStatus{returnCode})
	}

	return returnErr
}

func init() {
	addCommand("exec", shortExecHelp, longExecHelp, func() flags.Commander { return &cmdExec{} }, execDescs, nil)
}
