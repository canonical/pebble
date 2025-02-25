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

package daemon

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/plan"
)

type checkInfo struct {
	Name      string `json:"name"`
	Level     string `json:"level,omitempty"`
	Startup   string `json:"startup"`
	Status    string `json:"status"`
	Failures  int    `json:"failures,omitempty"`
	Threshold int    `json:"threshold"`
	ChangeID  string `json:"change-id,omitempty"`
}

func v1GetChecks(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()
	level := plan.CheckLevel(query.Get("level"))
	switch level {
	case plan.UnsetLevel, plan.AliveLevel, plan.ReadyLevel:
	default:
		return BadRequest(`level must be "alive" or "ready"`)
	}

	names := strutil.MultiCommaSeparatedList(query["names"])

	checkMgr := c.d.overlord.CheckManager()
	checks, err := checkMgr.Checks()
	if err != nil {
		return InternalError("%v", err)
	}

	infos := []checkInfo{} // if no checks, return [] instead of null
	for _, check := range checks {
		levelMatch := level == plan.UnsetLevel || level == check.Level
		namesMatch := len(names) == 0 || strutil.ListContains(names, check.Name)
		if levelMatch && namesMatch {
			info := checkInfo{
				Name:      check.Name,
				Level:     string(check.Level),
				Startup:   string(check.Startup),
				Status:    string(check.Status),
				Failures:  check.Failures,
				Threshold: check.Threshold,
				ChangeID:  check.ChangeID,
			}
			infos = append(infos, info)
		}
	}
	return SyncResponse(infos)
}

func v1PostChecks(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Action string   `json:"action"`
		Checks []string `json:"checks"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode data from request body: %v", err)
	}

	if len(payload.Checks) == 0 {
		return BadRequest("must specify checks for %s action", payload.Action)
	}

	checkmgr := c.d.overlord.CheckManager()

	var err error
	var changed []string
	switch payload.Action {
	case "start":
		changed, err = checkmgr.StartChecks(payload.Checks)
	case "stop":
		changed, err = checkmgr.StopChecks(payload.Checks)
	default:
		return BadRequest("invalid action %q", payload.Action)
	}
	if err != nil {
		if _, ok := err.(*checkstate.ChecksNotFound); ok {
			return BadRequest("cannot %s checks: %v", payload.Action, err)
		} else {
			return InternalError("cannot %s checks: %v", payload.Action, err)
		}
	}

	st := c.d.overlord.State()
	st.EnsureBefore(0) // start and stop tasks right away

	sort.Strings(changed)
	return SyncResponse(responsePayload{Changed: changed})
}

type responsePayload struct {
	Changed []string `json:"changed"`
}
