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
	"net/http"
	"os/exec"
	"time"

	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/overlord/cmdstate"
)

func v1PostExec(c *Command, req *http.Request, _ *userState) Response {
	var payload struct {
		Mode        string            `json:"mode"`
		Command     []string          `json:"command"`
		Environment map[string]string `json:"environment"`
		WorkingDir  string            `json:"working-dir"`
		Timeout     time.Duration     `json:"timeout"`
		UserID      *int              `json:"user-id"`
		User        string            `json:"user"`
		GroupID     *int              `json:"group-id"`
		Group       string            `json:"group"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
	}
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request body: %v", err)
	}
	if payload.Mode == "" {
		return statusBadRequest("must specify mode")
	}
	if payload.Mode != "streaming" && payload.Mode != "interactive" {
		return statusBadRequest(`mode must be "streaming" or "interactive"`)
	}
	if len(payload.Command) < 1 {
		return statusBadRequest("must specify command")
	}

	// Check up-front that the executable exists.
	_, err := exec.LookPath(payload.Command[0])
	if err != nil {
		return statusBadRequest("%v", err)
	}

	// Convert User/UserID and Group/GroupID combinations into raw uid/gid.
	uid, gid, err := osutil.NormalizeUidGid(payload.UserID, payload.GroupID, payload.User, payload.Group)
	if err != nil {
		return statusBadRequest("%v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	args := &cmdstate.ExecArgs{
		Mode:        payload.Mode,
		Command:     payload.Command,
		Environment: payload.Environment,
		WorkingDir:  payload.WorkingDir,
		Timeout:     payload.Timeout,
		UserID:      uid,
		GroupID:     gid,
		Width:       payload.Width,
		Height:      payload.Height,
	}
	change, metadata, err := cmdstate.Exec(st, args)
	if err != nil {
		return statusInternalError("cannot create exec change: %v", err)
	}

	stateEnsureBefore(st, 0) // start it right away

	result := map[string]interface{}{
		"environment":   metadata.Environment,
		"websocket-ids": metadata.WebsocketIDs,
		"working-dir":   metadata.WorkingDir,
	}
	return AsyncResponse(result, change.ID())
}
