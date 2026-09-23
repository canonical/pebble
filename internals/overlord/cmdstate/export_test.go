// Copyright (c) 2026 Canonical Ltd
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
	"github.com/gorilla/websocket"
)

// AddTestExecution inserts a fake execution into the manager's map and
// broadcasts the condition variable.
func (m *CommandManager) AddTestExecution(taskID string) {
	m.executionsCond.L.Lock()
	m.executions[taskID] = &execution{}
	m.executionsCond.Broadcast()
	m.executionsCond.L.Unlock()
}

// AddFakeExecution inserts an execution configured for testing into the
// manager's map and broadcasts the condition variable.
func (m *CommandManager) AddFakeExecution(taskID string, websocketIDs ...string) {
	websockets := make(map[string]*websocket.Conn, len(websocketIDs))
	for _, id := range websocketIDs {
		websockets[id] = nil
	}
	m.executionsCond.L.Lock()
	m.executions[taskID] = &execution{
		websockets:       websockets,
		controlConnected: make(chan struct{}),
		ioConnected:      make(chan struct{}),
	}
	m.executionsCond.Broadcast()
	m.executionsCond.L.Unlock()
}

// ExecutionWebsocket returns the connected websocket for the given task and
// websocket ID.
func (m *CommandManager) ExecutionWebsocket(taskID, websocketID string) *websocket.Conn {
	m.executionsCond.L.Lock()
	e := m.executions[taskID]
	m.executionsCond.L.Unlock()
	if e == nil {
		return nil
	}
	return e.getWebsocket(websocketID)
}
