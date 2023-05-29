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
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
)

// ExecArgs holds the arguments for a command execution.
type ExecArgs struct {
	Command     []string
	Environment map[string]string
	WorkingDir  string
	Timeout     time.Duration
	UserID      *int
	GroupID     *int
	Terminal    bool
	Interactive bool
	SplitStderr bool
	Width       int
	Height      int
}

// ExecMetadata is the metadata returned from an Exec call.
type ExecMetadata struct {
	TaskID      string
	Environment map[string]string
	WorkingDir  string
}

// execSetup is stored on a task to specify the args for an execution.
type execSetup struct {
	Command     []string
	Environment map[string]string
	Timeout     time.Duration
	Terminal    bool
	Interactive bool
	SplitStderr bool
	Width       int
	Height      int
	UserID      *int
	GroupID     *int
	WorkingDir  string
}

// Exec creates a task that will execute the command with the given arguments.
func Exec(st *state.State, args *ExecArgs) (*state.Task, ExecMetadata, error) {
	if args.Interactive && !args.Terminal {
		return nil, ExecMetadata{}, errors.New("cannot use interactive mode without a terminal")
	}

	environment := map[string]string{}
	for k, v := range args.Environment {
		environment[k] = v
	}

	// Set a reasonable default for PATH.
	_, ok := environment["PATH"]
	if !ok {
		environment["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	// Set HOME and USER based on the UserID.
	if environment["HOME"] == "" || environment["USER"] == "" {
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
			if environment["HOME"] == "" {
				environment["HOME"] = u.HomeDir
			}
			if environment["USER"] == "" {
				environment["USER"] = u.Username
			}
		}
	}

	// Set default value for LANG.
	_, ok = environment["LANG"]
	if !ok {
		environment["LANG"] = "C.UTF-8"
	}

	// Set default working directory to $HOME, or / if $HOME not set.
	workingDir := args.WorkingDir
	if workingDir == "" {
		workingDir = environment["HOME"]
		if workingDir == "" {
			workingDir = "/"
		}
	}

	// Create a task for this execution (though it's not started here).
	task := st.NewTask("exec", fmt.Sprintf("exec command %q", args.Command[0]))
	setup := execSetup{
		Command:     args.Command,
		Environment: environment,
		Timeout:     args.Timeout,
		Terminal:    args.Terminal,
		Interactive: args.Interactive,
		SplitStderr: args.SplitStderr,
		Width:       args.Width,
		Height:      args.Height,
		UserID:      args.UserID,
		GroupID:     args.GroupID,
		WorkingDir:  workingDir,
	}
	task.Set("exec-setup", &setup)

	metadata := ExecMetadata{
		TaskID:      task.ID(),
		Environment: environment,
		WorkingDir:  workingDir,
	}

	return task, metadata, nil
}
