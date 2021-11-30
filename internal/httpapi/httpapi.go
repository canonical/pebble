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
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/checkstate"
	"github.com/canonical/pebble/internal/plan"
)

type API struct {
	checkMgr CheckManager
	router   *mux.Router
}

type CheckManager interface {
	Checks(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error)
}

func NewAPI(checkMgr CheckManager) *API {
	s := &API{
		checkMgr: checkMgr,
		router:   mux.NewRouter(),
	}

	s.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	s.router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	s.router.HandleFunc("/v1/health", s.getHealth).Methods("GET")

	return s
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

func writeResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		logger.Noticef("Cannot marshal JSON: %v", err)
		http.Error(w, `{"error":"cannot marshal JSON"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, err = w.Write(b)
	if err != nil {
		// Very unlikely to happen, but log any error (not much more we can do)
		logger.Noticef("Cannot write JSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, error string) {
	writeResponse(w, status, errorResponse{Error: error})
}

type errorResponse struct {
	Error string `json:"error"`
}
