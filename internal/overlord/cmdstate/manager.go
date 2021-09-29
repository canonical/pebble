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
	"net/http"
	"sync"

	"github.com/canonical/pebble/internal/overlord/state"
)

type CommandManager struct {
	executions      map[string]*execution
	executionsMutex sync.Mutex
}

// NewManager creates a new CommandManager.
func NewManager(runner *state.TaskRunner) *CommandManager {
	manager := &CommandManager{
		executions: make(map[string]*execution),
	}
	runner.AddHandler("exec", manager.doExec, nil)
	return manager
}

// Ensure is part of the overlord.StateManager interface.
func (m *CommandManager) Ensure() error {
	return nil
}

// Connect upgrades the HTTP connection and connects to the given websocket.
func (m *CommandManager) Connect(r *http.Request, w http.ResponseWriter, task *state.Task, websocketID string) error {
	m.executionsMutex.Lock()
	e := m.executions[task.ID()]
	m.executionsMutex.Unlock()
	if e == nil {
		return fmt.Errorf("task %q has no execution object", task.ID())
	}
	return e.connect(r, w, websocketID)
}
