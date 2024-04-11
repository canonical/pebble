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

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/plan"
)

type healthInfo struct {
	Healthy bool `json:"healthy"`
}

func v1Health(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()
	level := plan.CheckLevel(query.Get("level"))
	switch level {
	case plan.UnsetLevel, plan.AliveLevel, plan.ReadyLevel:
	default:
		return healthError(http.StatusBadRequest, `level must be "alive" or "ready"`)
	}

	names := strutil.MultiCommaSeparatedList(query["names"])

	checks, err := getChecks(c.d.overlord)
	if err != nil {
		logger.Noticef("Cannot fetch checks: %v", err.Error())
		return healthError(http.StatusInternalServerError, "internal server error")
	}

	healthy := true
	status := http.StatusOK
	for _, check := range checks {
		levelMatch := level == plan.UnsetLevel || level == check.Level ||
			level == plan.ReadyLevel && check.Level == plan.AliveLevel // ready implies alive
		namesMatch := len(names) == 0 || strutil.ListContains(names, check.Name)
		if levelMatch && namesMatch && check.Status != checkstate.CheckStatusUp {
			healthy = false
			status = http.StatusBadGateway
		}
	}

	return SyncResponse(&healthResp{
		Type:       ResponseTypeSync,
		Status:     status,
		StatusText: http.StatusText(status),
		Result:     healthInfo{Healthy: healthy},
	})
}

// Like the resp struct, but without the warning/maintenance fields, so that
// the health endpoint doesn't have to acquire the state lock (resulting in a
// slow response on heavily-loaded systems).
type healthResp struct {
	Type       ResponseType `json:"type"`
	Status     int          `json:"status-code"`
	StatusText string       `json:"status,omitempty"`
	Result     interface{}  `json:"result,omitempty"`
}

func (r *healthResp) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	bs, err := json.Marshal(r)
	if err != nil {
		logger.Noticef("Cannot marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(bs)
}

func healthError(status int, message string) *healthResp {
	return &healthResp{
		Type:       ResponseTypeError,
		Status:     status,
		StatusText: http.StatusText(status),
		Result:     &errorResult{Message: message},
	}
}
