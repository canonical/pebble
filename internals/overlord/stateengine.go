// Copyright (c) 2014-2020 Canonical Ltd
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

package overlord

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
)

// StateManager is implemented by types responsible for observing
// the system and manipulating it to reflect the desired state.
type StateManager interface {
	// DryStart prepares the manager to run its activities without
	// incurring unwanted side effects.
	DryStart() error

	// Ensure forces a complete evaluation of the current state.
	// See StateEngine.Ensure for more details.
	Ensure() error
}

// StateWaiter is optionally implemented by StateManagers that have running
// activities that can be waited.
type StateWaiter interface {
	// Wait asks manager to wait for all running activities to finish.
	Wait()
}

// StateStopper is optionally implemented by StateManagers that have
// running activities that can be terminated.
type StateStopper interface {
	// Stop asks the manager to terminate all activities running
	// concurrently.  It must not return before these activities
	// are finished.
	Stop()
}

// StateEngine controls the dispatching of state changes to state managers.
//
// Most of the actual work performed by the state engine is in fact done
// by the individual managers registered. These managers must be able to
// cope with Ensure calls in any order, coordinating among themselves
// solely via the state.
type StateEngine struct {
	state      *state.State
	dryStarted bool
	stopped    bool
	// managers in use
	mgrLock  sync.Mutex
	managers []StateManager
}

// NewStateEngine returns a new state engine.
func NewStateEngine(s *state.State) *StateEngine {
	return &StateEngine{
		state: s,
	}
}

// State returns the current system state.
func (se *StateEngine) State() *state.State {
	return se.state
}

// multiError collects multiple errors that affected an operation.
type multiError struct {
	header string
	errs   []error
}

// newMultiError returns a new multiError struct initialized with
// the given format string that explains what operation potentially
// went wrong. multiError can be nested and will render correctly
// in these cases.
func newMultiError(header string, errs []error) error {
	return &multiError{header: header, errs: errs}
}

// Error formats the error string.
func (me *multiError) Error() string {
	return me.nestedError(0)
}

// helper to ensure formating of nested multiErrors works.
func (me *multiError) nestedError(level int) string {
	indent := strings.Repeat(" ", level)
	buf := bytes.NewBufferString(fmt.Sprintf("%s:\n", me.header))
	if level > 8 {
		return "circular or too deep error nesting (max 8)?!"
	}
	for i, err := range me.errs {
		switch v := err.(type) {
		case *multiError:
			fmt.Fprintf(buf, "%s- %v", indent, v.nestedError(level+1))
		default:
			fmt.Fprintf(buf, "%s- %v", indent, err)
		}
		if i < len(me.errs)-1 {
			fmt.Fprintf(buf, "\n")
		}
	}
	return buf.String()
}

var errStateEngineStopped = errors.New("state engine already stopped")

// DryStart ensures all managers are ready to run their activities
// before the first call to Ensure() is made.
func (se *StateEngine) DryStart() error {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.stopped {
		return errStateEngineStopped
	}
	var errs []error
	for _, m := range se.managers {
		err := m.DryStart()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return newMultiError("dry-start failed", errs)
	}
	se.dryStarted = true
	return nil
}

// Ensure asks every manager to ensure that they are doing the necessary
// work to put the current desired system state in place by calling their
// respective Ensure methods.
//
// Managers must evaluate the desired state completely when they receive
// that request, and report whether they found any critical issues. They
// must not perform long running activities during that operation, though.
// These should be performed in properly tracked changes and tasks.
func (se *StateEngine) Ensure() error {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if !se.dryStarted {
		return errors.New("state engine did not dry-start")
	}
	if se.stopped {
		return errStateEngineStopped
	}
	var errs []error
	for _, m := range se.managers {
		err := m.Ensure()
		if err != nil {
			logger.Noticef("state ensure error: %v", err)
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return newMultiError("state ensure errors", errs)
	}
	return nil
}

// AddManager adds the provided manager to take part in state operations.
func (se *StateEngine) AddManager(m StateManager) {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	se.managers = append(se.managers, m)
}

// Wait waits for all managers current activities.
func (se *StateEngine) Wait() {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.stopped {
		return
	}
	for _, m := range se.managers {
		if waiter, ok := m.(StateWaiter); ok {
			waiter.Wait()
		}
	}
}

// Stop asks all managers to terminate activities running concurrently.
func (se *StateEngine) Stop() {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.stopped {
		return
	}
	for _, m := range se.managers {
		if stopper, ok := m.(StateStopper); ok {
			stopper.Stop()
		}
	}
	se.stopped = true
}
