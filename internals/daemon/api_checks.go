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
	"sort"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/plan"
)

type checkInfo struct {
	Name      string `json:"name"`
	Level     string `json:"level,omitempty"`
	Startup   string `json:"startup"`
	Status    string `json:"status"`
	Successes int    `json:"successes"`
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
			info := checkInfoFromInternal(check)
			infos = append(infos, info)
		}
	}
	return SyncResponse(infos)
}

func v1PostChecks(c *Command, r *http.Request, user *UserState) Response {
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
		for _, check := range payload.Checks {
			logger.SecurityWarn(logger.SecuritySysMonitorDisabled,
				fmt.Sprintf("%s,%s", userString(user), check),
				fmt.Sprintf("Stopping check %s", check))
		}
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

	if changed == nil {
		changed = []string{} // send JSON "[]" instead of "null" if nothing's changed
	}
	sort.Strings(changed)
	return SyncResponse(responsePayload{Changed: changed})
}

func v1PostChecksRefresh(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode data from request body: %v", err)
	}

	if payload.Name == "" {
		return BadRequest("must specify check name")
	}

	plan := c.d.overlord.PlanManager().Plan()
	check, ok := plan.Checks[payload.Name]
	if !ok {
		return NotFound("cannot find check with name %q", payload.Name)
	}

	checkMgr := c.d.overlord.CheckManager()
	result, err := checkMgr.RefreshCheck(r.Context(), check)
	info := checkInfoFromInternal(result)
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	return SyncResponse(refreshPayload{Info: info, Error: errStr})
}

type refreshPayload struct {
	Info  checkInfo `json:"info"`
	Error string    `json:"error,omitempty"`
}

type responsePayload struct {
	Changed []string `json:"changed"`
}

func checkInfoFromInternal(check *checkstate.CheckInfo) checkInfo {
	return checkInfo{
		Name:      check.Name,
		Level:     string(check.Level),
		Startup:   string(check.Startup),
		Status:    string(check.Status),
		Successes: check.Successes,
		Failures:  check.Failures,
		Threshold: check.Threshold,
		ChangeID:  check.ChangeID,
	}
}
