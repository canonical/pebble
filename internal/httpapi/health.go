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

package httpapi

import (
	"net/http"

	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/strutil"
)

func (a *API) getHealth(w http.ResponseWriter, r *http.Request) {
	level := plan.CheckLevel(r.URL.Query().Get("level"))
	switch level {
	case plan.UnsetLevel, plan.AliveLevel, plan.ReadyLevel:
	default:
		writeError(w, http.StatusBadRequest, `level must be "alive" or "ready"`)
		return
	}

	names := r.URL.Query()["names"]

	checks, err := a.checkMgr.Checks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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

	writeResponse(w, status, healthResponse{Healthy: healthy})
}

type healthResponse struct {
	Healthy bool `json:"healthy"`
}
