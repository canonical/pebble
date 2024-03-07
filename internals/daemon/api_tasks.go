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

package daemon

import (
	"errors"
	"net/http"
	"os"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
)

func v1GetTaskWebsocket(c *Command, req *http.Request, _ *UserState) Response {
	vars := muxVars(req)
	taskID := vars["task-id"]
	websocketID := vars["websocket-id"]

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	task := st.Task(taskID)
	if task == nil {
		// These errors are logged as well, because when a client is
		// connecting to a websocket they may only see the error
		// "bad handshake".
		logger.Noticef("Websocket: cannot find task with id %q", taskID)
		return NotFound("cannot find task with id %q", taskID)
	}

	var connect websocketConnectFunc
	switch task.Kind() {
	case "exec":
		commandMgr := c.d.overlord.CommandManager()
		connect = commandMgr.Connect
	default:
		logger.Noticef("Websocket %s: %q tasks do not have websockets", task.ID(), task.Kind())
		return BadRequest("%q tasks do not have websockets", task.Kind())
	}

	return websocketResponse{
		task:        task,
		websocketID: websocketID,
		connect:     connect,
	}
}

type websocketConnectFunc func(r *http.Request, w http.ResponseWriter, task *state.Task, websocketID string) error

type websocketResponse struct {
	task        *state.Task
	websocketID string
	connect     websocketConnectFunc
}

func (wr websocketResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := wr.connect(r, w, wr.task, wr.websocketID)
	if errors.Is(err, os.ErrNotExist) {
		logger.Noticef("Websocket %s: cannot find websocket with id %q", wr.task.ID(), wr.websocketID)
		rsp := NotFound("cannot find websocket with id %q", wr.websocketID)
		rsp.ServeHTTP(w, r)
		return
	}
	if err != nil {
		logger.Noticef("Websocket %s: cannot connect to websocket %q: %v", wr.task.ID(), wr.websocketID, err)
		rsp := InternalError("cannot connect to websocket %q: %v", wr.websocketID, err)
		rsp.ServeHTTP(w, r)
		return
	}
	// In the success case, Connect takes over the connection and upgrades to
	// the websocket protocol.
}
