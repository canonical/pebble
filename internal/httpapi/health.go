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
)

func (a *API) getHealth(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	switch level {
	case "", "alive", "ready":
	default:
		writeError(w, http.StatusBadRequest, `level must be "alive" or "ready"`)
		return
	}

	names := r.URL.Query()["names"]

	checks, err := a.checkMgr.Checks(plan.CheckLevel(level), names)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	healthy := true
	for _, check := range checks {
		levelMatch := level == "" || level == string(check.Level) || (level == "alive" && check.Level == plan.ReadyLevel)
		if !levelMatch {
			continue
		}
		if !check.Healthy {
			healthy = false
		}
	}

	writeResponse(w, http.StatusOK, healthResponse{Healthy: healthy})
}

type healthResponse struct {
	Healthy bool `json:"healthy"`
}
