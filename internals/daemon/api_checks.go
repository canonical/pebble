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

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/plan"
)

type checkInfo struct {
	Name      string `json:"name"`
	Level     string `json:"level,omitempty"`
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

func v1PostChecksRun(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Check string `json:"check"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode data from request body: %v", err)
	}

	if payload.Check == "" {
		return BadRequest("no check provided")
	}

	check, ok := c.d.overlord.PlanManager().Plan().Checks[payload.Check]
	if !ok {
		return SyncResponse("check not found")
	}

	err := c.d.overlord.CheckManager().RunCheck(r.Context(), check)
	if err != nil {
		return SyncResponse(err)
	}
	return SyncResponse("check passed")
}
