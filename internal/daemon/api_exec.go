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
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/overlord/cmdstate"
	"github.com/canonical/pebble/internal/overlord/state"
)

func v1PostExec(c *Command, req *http.Request, _ *userState) Response {
	var payload struct {
		Command     []string          `json:"command"`
		Environment map[string]string `json:"environment"`
		WorkingDir  string            `json:"working-dir"`
		UserID      *int              `json:"user-id"`
		User        string            `json:"user"`
		GroupID     *int              `json:"group-id"`
		Group       string            `json:"group"`
		// TODO: add timeout?
	}
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request body: %v", err)
	}
	if len(payload.Command) < 1 {
		return statusBadRequest("must specify command")
	}
	// TODO: check up-front that binary exists (and is runnable?)

	uid, gid, err := osutil.NormalizeUidGid(payload.UserID, payload.GroupID, payload.User, payload.Group)
	if err != nil {
		return statusBadRequest("%s", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	args := &cmdstate.ExecArgs{
		Command:     payload.Command,
		Environment: payload.Environment,
		WorkingDir:  payload.WorkingDir,
		UserID:      uid,
		GroupID:     gid,
	}
	taskSet, metadata, err := cmdstate.Exec(st, args)
	if err != nil {
		return statusInternalError("cannot create task: %v", err)
	}

	summary := fmt.Sprintf("Execute command %q", payload.Command[0])
	change := newChange(st, "exec", summary, []*state.TaskSet{taskSet}, nil)

	stateEnsureBefore(st, 0)

	return AsyncResponse(metadata, change.ID())
}

func v1GetExecWebsocket(c *Command, req *http.Request, _ *userState) Response {
	changeID := muxVars(req)["change-id"]

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	change := st.Change(changeID)
	if change == nil {
		return statusNotFound("cannot find change with id %q", changeID)
	}
	if len(change.Tasks()) < 1 {
		return statusInternalError("change %q has no tasks", changeID)
	}

	task := change.Tasks()[0]
	var cacheKey string
	err := task.Get("cache-key", &cacheKey)
	if err != nil {
		return statusInternalError("cannot get cache key: %v", err)
	}

	return websocketResponse{st: st, cacheKey: cacheKey}
}

type websocketResponse struct {
	st       *state.State
	cacheKey string
}

func (wr websocketResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: not certain about error handling here (what about 400's?)
	logger.Noticef("TODO: websocketResponse.ServeHTTP, secret=%s", r.FormValue("secret"))
	err := cmdstate.Connect(wr.st, wr.cacheKey, r, w)
	if err == os.ErrPermission {
		rsp := statusForbidden("incorrect secret")
		rsp.ServeHTTP(w, r)
		return
	}
	if err != nil {
		logger.Errorf("TODO websocketResponse.ServeHTTP err=%v", err)
		rsp := statusInternalError("%s", err)
		rsp.ServeHTTP(w, r)
		return
	}
}
