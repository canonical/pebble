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
	"fmt"
	"net/http"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/plan"
)

type checkInfo struct {
	Name      string `json:"name"`
	Level     string `json:"level,omitempty"`
	Startup   string `json:"startup,omitempty"`
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

	var err error
	var checks []string
	checkmgr := c.d.overlord.CheckManager()
	plan := c.d.overlord.PlanManager().Plan()

	switch payload.Action {
	case "start":
		checks, err = checkmgr.StartChecks(plan, payload.Checks)
	case "stop":
		checks, err = checkmgr.StopChecks(plan, payload.Checks)
	default:
		return BadRequest("invalid action %q", payload.Action)
	}
	if err != nil {
		return BadRequest("cannot %s checks: %v", payload.Action, err)
	}

	st := c.d.overlord.State()
	st.EnsureBefore(0) // start and stop tasks right away

	// TODO: figure out what the response should be - nothing? A message? If not
	// nothing, then it ought to be a JSON payload, so what's the format. It's
	// all messy right now, and the cmd_*-checks.go files need to be aligned as
	// well. Maybe there is an existing return object like BadRequest but good
	// that would be appropriate?

	var result string
	if len(checks) == 0 {
		result = fmt.Sprintf("No checks needed to %s", payload.Action)
	} else if len(checks) == 1 {
		result = fmt.Sprintf("Queued %s for check %q", payload.Action, checks[0])
	} else {
		result = fmt.Sprintf("Queued %s for check %q and %d more", payload.Action, checks[0], len(checks)-1)
	}
	return SyncResponse(result)
}
