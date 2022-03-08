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
	"net/http"

	"github.com/gorilla/mux"

	"github.com/canonical/pebble/internal/overlord"
	"github.com/canonical/pebble/internal/overlord/restart"
	"github.com/canonical/pebble/internal/overlord/state"
)

var api = []*Command{{
	// See daemon.go:canAccess for details how the access is controlled.
	Path:    "/v1/system-info",
	GuestOK: true,
	GET:     v1SystemInfo,
}, {
	Path:    "/v1/health",
	GuestOK: true,
	GET:     v1Health,
}, {
	Path:   "/v1/warnings",
	UserOK: true,
	GET:    v1GetWarnings,
	POST:   v1AckWarnings,
}, {
	Path:   "/v1/changes",
	UserOK: true,
	GET:    v1GetChanges,
}, {
	Path:   "/v1/changes/{id}",
	UserOK: true,
	GET:    v1GetChange,
	POST:   v1PostChange,
}, {
	Path:   "/v1/changes/{id}/wait",
	UserOK: true,
	GET:    v1GetChangeWait,
}, {
	Path:   "/v1/services",
	UserOK: true,
	GET:    v1GetServices,
	POST:   v1PostServices,
}, {
	Path:   "/v1/services/{name}",
	UserOK: true,
	GET:    v1GetService,
	POST:   v1PostService,
}, {
	Path:   "/v1/plan",
	UserOK: true,
	GET:    v1GetPlan,
}, {
	Path:   "/v1/layers",
	UserOK: true,
	POST:   v1PostLayers,
}, {
	Path:   "/v1/files",
	UserOK: true,
	GET:    v1GetFiles,
	POST:   v1PostFiles,
}, {
	Path:   "/v1/logs",
	UserOK: true,
	GET:    v1GetLogs,
}, {
	Path:   "/v1/exec",
	UserOK: true,
	POST:   v1PostExec,
}, {
	Path:   "/v1/tasks/{task-id}/websocket/{websocket-id}",
	UserOK: true,
	GET:    v1GetTaskWebsocket,
}, {
	Path:   "/v1/signals",
	UserOK: true,
	POST:   v1PostSignals,
}, {
	Path:   "/v1/checks",
	UserOK: true,
	GET:    v1GetChecks,
}}

var (
	stateOkayWarnings    = (*state.State).OkayWarnings
	stateAllWarnings     = (*state.State).AllWarnings
	statePendingWarnings = (*state.State).PendingWarnings
	stateEnsureBefore    = (*state.State).EnsureBefore

	overlordServiceManager = (*overlord.Overlord).ServiceManager

	muxVars = mux.Vars
)

func v1SystemInfo(c *Command, r *http.Request, _ *userState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	result := map[string]interface{}{
		"version": c.d.Version,
		"boot-id": restart.BootID(state),
	}
	return SyncResponse(result)
}
