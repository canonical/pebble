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

package servstate

import (
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/internal/logger"
)

// setChildSubreaper sets the current process as a "child subreaper" so we
// become the parent of dead child processes rather than PID 1. This allows us
// to wait for processes that are started by a Pebble service but then die, to
// "reap" them (see https://unix.stackexchange.com/a/250156/73491).
//
// The function returns true if sub-reaping is available (Linux 3.4+) along
// with an error if it's available but can't be set.
func setChildSubreaper() (bool, error) {
	err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
	if err == unix.EINVAL {
		return false, nil
	}
	return true, err
}

// reapChildren "reaps" (waits for) child processes whose parents didn't
// wait() for them. It stops when the stop channel is closed.
func reapChildren(stop <-chan struct{}) {
	sigChld := make(chan os.Signal, 1)
	signal.Notify(sigChld, unix.SIGCHLD)
	for {
		logger.Debugf("Reaper waiting for SIGCHLD")
		select {
		case <-sigChld:
			logger.Debugf("Reaper received SIGCHLD")

			// This allows ServiceManager's cmd.Wait() to pick it up before
			// the reaper for normal service process exits, and avoid
			// returning a "waitid: no child processes" error. A bit hacky,
			// but I don't know another simple way to prevent this.
			time.Sleep(25 * time.Millisecond)

			reapOnce()
		case <-stop:
			signal.Reset(unix.SIGCHLD)
			logger.Debugf("Reaper stopped")
			return
		}
	}
}

// reapOnce waits for zombie child processes until there are no more.
func reapOnce() {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
		switch err {
		case nil:
			if pid <= 0 {
				logger.Debugf("Reaper found no children have changed state")
				return
			}
			logger.Noticef("Reaped child PID %d", pid)

		case unix.ECHILD:
			logger.Debugf("Reaper has no more children to wait for")
			return

		default:
			logger.Noticef("Cannot wait for children: %v", err)
			return
		}
	}
}
