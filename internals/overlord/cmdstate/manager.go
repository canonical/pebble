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
	"net/http"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/overlord/state"
)

type CommandManager struct {
	executions     map[string]*execution
	executionsCond *sync.Cond
}

// NewManager creates a new CommandManager.
func NewManager(runner *state.TaskRunner) *CommandManager {
	manager := &CommandManager{
		executions:     make(map[string]*execution),
		executionsCond: sync.NewCond(&sync.Mutex{}),
	}
	runner.AddHandler("exec", manager.doExec, nil)

	// Delete the in-memory execSetup object when the exec is done.
	runner.AddCleanup("exec", func(task *state.Task, tomb *tomb.Tomb) error {
		st := task.State()
		st.Lock()
		defer st.Unlock()
		st.Cache(execSetupKey{task.ID()}, nil)
		return nil
	})

	return manager
}

type execSetupKey struct {
	taskID string
}

// Ensure is part of the overlord.StateManager interface.
func (m *CommandManager) Ensure() error {
	return nil
}

// Connect upgrades the HTTP connection and connects to the given websocket.
func (m *CommandManager) Connect(r *http.Request, w http.ResponseWriter, task *state.Task, websocketID string) error {
	stopWait := make(chan struct{})
	defer func() {
		// So waitExecution wakes up if it's stuck in Wait().
		m.executionsCond.L.Lock()
		close(stopWait)
		m.executionsCond.Broadcast()
		m.executionsCond.L.Unlock()
	}()

	executionCh := make(chan *execution)
	go func() {
		e := m.waitExecution(task.ID(), stopWait)
		if e != nil {
			executionCh <- e
		}
	}()

	st := task.State()
	st.Lock()
	change := task.Change()
	st.Unlock()

	// Wait till the execution object is ready or the request is cancelled.
	select {
	case e := <-executionCh:
		return e.connect(r, w, websocketID)
	case <-r.Context().Done():
		return r.Context().Err()
	// Change unexpectedly marked ready, probably due to the client not sending
	// websocket requests in time.
	case <-change.Ready():
		st.Lock()
		defer st.Unlock()
		return change.Err()
	}
}

func (m *CommandManager) waitExecution(taskID string, stop <-chan struct{}) *execution {
	m.executionsCond.L.Lock()
	defer m.executionsCond.L.Unlock()

	for {
		select {
		case <-stop:
			return nil
		default:
		}

		e := m.executions[taskID]
		if e != nil {
			return e
		}
		m.executionsCond.Wait()
	}
}
