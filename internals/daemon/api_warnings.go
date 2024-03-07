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

package daemon

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/canonical/pebble/internals/overlord/state"
)

func v1AckWarnings(c *Command, r *http.Request, _ *UserState) Response {
	defer r.Body.Close()
	var op struct {
		Action    string    `json:"action"`
		Timestamp time.Time `json:"timestamp"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&op); err != nil {
		return BadRequest("cannot decode request body into warnings operation: %v", err)
	}
	if op.Action != "okay" {
		return BadRequest("unknown warning action %q", op.Action)
	}
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	n := stateOkayWarnings(st, op.Timestamp)

	return SyncResponse(n)
}

func v1GetWarnings(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()
	var all bool
	sel := query.Get("select")
	switch sel {
	case "all":
		all = true
	case "pending", "":
		all = false
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var ws []*state.Warning
	if all {
		ws = stateAllWarnings(st)
	} else {
		ws, _ = statePendingWarnings(st)
	}
	if len(ws) == 0 {
		// no need to confuse the issue
		return SyncResponse([]state.Warning{})
	}

	return SyncResponse(ws)
}
