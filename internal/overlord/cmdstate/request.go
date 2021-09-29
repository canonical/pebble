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
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/strutil"
	"github.com/gorilla/websocket"
)

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
	WebsocketIDs map[string]string // keys are "control", "stdio", as well as "stderr" if SplitStderr true
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
			logger.Noticef("Cannot look up user %d: %v", userID, err)
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
	e.websockets[wsStdio] = nil
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
