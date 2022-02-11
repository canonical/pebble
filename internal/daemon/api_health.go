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
	"net/http"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/strutil"
)

type healthInfo struct {
	Healthy bool `json:"healthy"`
}

func v1Health(c *Command, r *http.Request, _ *userState) Response {
	level := plan.CheckLevel(r.URL.Query().Get("level"))
	switch level {
	case plan.UnsetLevel, plan.AliveLevel, plan.ReadyLevel:
	default:
		return statusBadRequest(`level must be "alive" or "ready"`)
	}

	names := r.URL.Query()["names"]

	checks, err := c.d.CheckManager().Checks()
	if err != nil {
		logger.Noticef("Cannot fetch checks: %v", err.Error())
		return statusInternalError("internal server error")
	}

	healthy := true
	status := http.StatusOK
	for _, check := range checks {
		levelMatch := level == plan.UnsetLevel || level == check.Level ||
			level == plan.ReadyLevel && check.Level == plan.AliveLevel // ready implies alive
		namesMatch := len(names) == 0 || strutil.ListContains(names, check.Name)
		if levelMatch && namesMatch && !check.Healthy {
			healthy = false
			status = http.StatusBadGateway
		}
	}

	return SyncResponse(&resp{
		Type:   ResponseTypeSync,
		Status: status,
		Result: healthInfo{Healthy: healthy},
	})
}
