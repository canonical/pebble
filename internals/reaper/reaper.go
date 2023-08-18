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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"

	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
)

var (
	reaperTomb tomb.Tomb

	mutex   sync.Mutex
	pids    = make(map[int]chan int)
	started bool
)

// Start starts the child process reaper.
func Start() error {
	mutex.Lock()
	defer mutex.Unlock()

	if started {
		return nil // already started
	}

	isSubreaper, err := setChildSubreaper()
	if err != nil {
		return fmt.Errorf("cannot set child subreaper: %w", err)
	}
	if !isSubreaper {
		return fmt.Errorf("child subreaping unavailable on this platform")
	}

	started = true
	reaperTomb.Go(reapChildren)
	return nil
}

// Stop stops the child process reaper.
func Stop() error {
	mutex.Lock()
	if !started {
		mutex.Unlock()
		return nil // already stopped
	}
	mutex.Unlock()

	reaperTomb.Kill(nil)
	reaperTomb.Wait()
	reaperTomb = tomb.Tomb{}

	mutex.Lock()
	started = false
	mutex.Unlock()

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
// wait() for them. It stops when the reaper tomb is killed.
func reapChildren() error {
	logger.Debugf("Reaper started, waiting for SIGCHLD.")
	sigChld := make(chan os.Signal, 1)
	signal.Notify(sigChld, unix.SIGCHLD)
	for {
		select {
		case <-sigChld:
			logger.Debugf("Reaper received SIGCHLD.")
			reapOnce()
		case <-reaperTomb.Dying():
			signal.Reset(unix.SIGCHLD)
			logger.Debugf("Reaper stopped.")
			return nil
		}
	}
}

// reapOnce waits for child processes until there are no more to reap.
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
			mutex.Lock()
			ch := pids[pid]
			mutex.Unlock()

			if ch != nil {
				ch <- exitCode
			}

		case unix.ECHILD:
			return

		default:
			logger.Noticef("Cannot wait for child process: %v", err)
			return
		}
	}
}

// StartCommand starts the command and registers its PID with the reaper.
//
// After the reaper has been started, users of os/exec should call WaitCommand
// and StartCommand rather than cmd.Wait directly, to ensure PIDs are reaped
// correctly.
func StartCommand(cmd *exec.Cmd) error {
	// Must lock for the cmd.Start call and the insertion into the PIDs map,
	// to avoid the reaper firing before we've registered the PID for a
	// process that exits quickly.
	mutex.Lock()
	defer mutex.Unlock()

	if !started {
		panic("internal error: reaper must be started")
	}

	err := cmd.Start()
	if err == nil {
		if ch, ok := pids[cmd.Process.Pid]; ok {
			// Shouldn't happen, but just in case we get the same PID we're
			// already waiting on, tell the other waiter to stop waiting.
			select {
			case ch <- -1:
			default:
			}
			logger.Noticef("Internal error: new PID %d observed while still being tracked", cmd.Process.Pid)
		}
		// Channel is 1-buffered so the send in reapOnce never blocks, if for
		// some reason someone forgets to call WaitCommand.
		pids[cmd.Process.Pid] = make(chan int, 1)
	}
	return err
}

// WaitCommand waits for the command (which must have been started with
// StartCommand) to finish and returns its exit code. Unlike cmd.Wait,
// WaitCommand doesn't return an error for nonzero exit codes.
func WaitCommand(cmd *exec.Cmd) (int, error) {
	mutex.Lock()
	if !started {
		mutex.Unlock()
		panic("internal error: reaper must be started")
	}
	ch, ok := pids[cmd.Process.Pid]
	if !ok {
		// Shouldn't happen, but doesn't hurt to handle it.
		mutex.Unlock()
		return -1, fmt.Errorf("internal error: PID %d was not started with WaitCommand", cmd.Process.Pid)
	}
	mutex.Unlock()

	// Wait for reaper to reap this PID and send us the exit code.
	exitCode := <-ch

	// Remove PID from waits map once we've received exit code from reaper.
	mutex.Lock()
	delete(pids, cmd.Process.Pid)
	mutex.Unlock()

	// At this point, we expect cmd.Wait to return a syscall error ("wait[id]:
	// no child processes"), because the reaper is already waiting for all
	// PIDs. This is not pretty, but we need to call cmd.Wait to clean up
	// goroutines and file descriptors.
	err := cmd.Wait()
	switch err := err.(type) {
	case nil:
		logger.Noticef("Internal error: WaitCommand expected error but got nil (exit code %d)", exitCode)
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

// CommandCombinedOutput is like cmd.CombinedOutput, but for use when the
// reaper is running.
func CommandCombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	mutex.Lock()
	if !started {
		mutex.Unlock()
		panic("internal error: reaper must be started")
	}
	mutex.Unlock()

	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := StartCommand(cmd)
	if err != nil {
		return nil, err
	}
	exitCode, err := WaitCommand(cmd)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("exit status %d", exitCode)
	}
	return b.Bytes(), err
}
