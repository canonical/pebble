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
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
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

	// Inherit the pebble daemon environment.
	// If the user is being changed, unset the HOME and USER env vars so that they
	// can be set correctly later on in this method.
	environment := osutil.Environ()
	if args.UserID != nil && *args.UserID != os.Getuid() {
		delete(environment, "HOME")
		delete(environment, "USER")
	}

	for k, v := range args.Environment {
		// Requested environment takes precedence.
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

	workingDir, err := getWorkingDir(args.WorkingDir, environment["HOME"])
	if err != nil {
		return nil, ExecMetadata{}, err
	}

	// Create a task for this execution (though it's not started here).
	task := st.NewTask("exec", fmt.Sprintf("Execute command %q", args.Command[0]))
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

// getWorkingDir calculates the working directory using the working-dir
// argument, or $HOME if that's not set, or "/" if $HOME is not set.
func getWorkingDir(workingDir, homeDir string) (string, error) {
	dirName := "working directory"
	if workingDir == "" {
		if homeDir == "" {
			return "/", nil
		}
		workingDir = homeDir
		dirName = "home directory"
	}
	// Check that the working directory exists, to avoid a confusing error
	// message later that implies the command doesn't exist, for example:
	//
	//  fork/exec /usr/local/bin/realcommand: no such file or directory
	st, err := os.Stat(workingDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%s %q does not exist", dirName, workingDir)
	case err != nil:
		return "", fmt.Errorf("cannot stat %s %q: %w", dirName, workingDir, err)
	case !st.IsDir():
		return "", fmt.Errorf("%s %q not a directory", dirName, workingDir)
	}
	return workingDir, nil
}
