// Copyright (c) 2022 Canonical Ltd
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

package reaper

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/internal/logger"
)

var (
	stop    chan struct{}
	stopped chan struct{}

	waitsMutex sync.Mutex
	waits      = make(map[int]chan int)
)

// Start starts the child process reaper.
func Start() error {
	if stop != nil {
		return nil // already started
	}

	isSubreaper, err := setChildSubreaper()
	if err != nil {
		return fmt.Errorf("cannot set child subreaper: %w", err)
	}
	if !isSubreaper {
		return fmt.Errorf("child subreaping unavailable on this platform")
	}

	stop = make(chan struct{})
	stopped = make(chan struct{})
	go func() {
		reapChildren(stop)
		close(stopped)
	}()
	return nil
}

// Stop stops the child process reaper.
func Stop() error {
	if stop == nil {
		return nil // already stopped
	}
	close(stop)
	<-stopped // wait till reapChildren actually finishes
	stop = nil
	return nil
}

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
	logger.Debugf("Reaper started, waiting for SIGCHLD.")
	sigChld := make(chan os.Signal, 1)
	signal.Notify(sigChld, unix.SIGCHLD)
	for {
		select {
		case <-sigChld:
			logger.Debugf("Reaper received SIGCHLD.")
			reapOnce()
		case <-stop:
			signal.Reset(unix.SIGCHLD)
			logger.Debugf("Reaper stopped.")
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
				return
			}

			exitCode := status.ExitStatus()
			if status.Signaled() {
				exitCode = 128 + int(status.Signal())
			}
			logger.Debugf("Reaped PID %d which exited with code %d.", pid, exitCode)

			// If there's a WaitCommand waiting for this PID, send it the exit code.
			waitsMutex.Lock()
			ch := waits[pid]
			delete(waits, pid)
			waitsMutex.Unlock()
			if ch != nil {
				ch <- exitCode
			}

		case unix.ECHILD:
			return

		default:
			logger.Noticef("Reaper cannot wait for children: %v", err)
			return
		}
	}
}

// WaitCommand waits for the command to finish and returns its exit code.
// After the reaper has been started, users of os/exec should call this rather
// than cmd.Wait directly, to ensure PIDs are reaped correctly.
func WaitCommand(cmd *exec.Cmd) (int, error) {
	// Create a wait channel to tell reaper to notify us about this PID.
	ch := make(chan int)
	waitsMutex.Lock()
	if _, exists := waits[cmd.Process.Pid]; exists {
		waitsMutex.Unlock()
		return -1, fmt.Errorf("PID %d is already being waited on", cmd.Process.Pid)
	}
	waits[cmd.Process.Pid] = ch
	waitsMutex.Unlock()

	// Wait for reaper to reap this PID and send us the exit code.
	exitCode := <-ch

	// At this point, we expect cmd.Wait() to return a wait() syscall error
	// ("waitid: no child processes"), because the reaper is already waiting
	// for all PIDs. This is not pretty, but we need to call cmd.Wait() to
	// clean up goroutines and file descriptors.
	err := cmd.Wait()
	switch err := err.(type) {
	case nil:
		logger.Debugf("WaitCommand expected error, got nil (exit code %d)", exitCode)
		return exitCode, nil
	case *os.SyscallError:
		if err.Syscall == "wait" || err.Syscall == "waitid" {
			return exitCode, nil
		}
		return -1, err
	default:
		return -1, err
	}
}
