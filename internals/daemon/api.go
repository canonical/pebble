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

	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
)

var API = []*Command{{
	Path:       "/v1/system-info",
	ReadAccess: OpenAccess{},
	GET:        v1SystemInfo,
}, {
	Path:       "/v1/health",
	ReadAccess: OpenAccess{},
	GET:        v1Health,
}, {
	Path:        "/v1/warnings",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetWarnings,
	POST:        v1AckWarnings,
}, {
	Path:       "/v1/changes",
	ReadAccess: UserAccess{},
	GET:        v1GetChanges,
}, {
	Path:        "/v1/changes/{id}",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetChange,
	POST:        v1PostChange,
}, {
	Path:       "/v1/changes/{id}/wait",
	ReadAccess: UserAccess{},
	GET:        v1GetChangeWait,
}, {
	Path:        "/v1/services",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetServices,
	POST:        v1PostServices,
}, {
	Path:        "/v1/services/{name}",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetService,
	POST:        v1PostService,
}, {
	Path:       "/v1/plan",
	ReadAccess: UserAccess{},
	GET:        v1GetPlan,
}, {
	Path:        "/v1/layers",
	WriteAccess: UserAccess{},
	POST:        v1PostLayers,
}, {
	Path:        "/v1/files",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetFiles,
	POST:        v1PostFiles,
}, {
	Path:       "/v1/logs",
	ReadAccess: UserAccess{},
	GET:        v1GetLogs,
}, {
	Path:        "/v1/exec",
	WriteAccess: UserAccess{},
	POST:        v1PostExec,
}, {
	Path:       "/v1/tasks/{task-id}/websocket/{websocket-id}",
	ReadAccess: UserAccess{},
	GET:        v1GetTaskWebsocket,
}, {
	Path:        "/v1/signals",
	WriteAccess: UserAccess{},
	POST:        v1PostSignals,
}, {
	Path:       "/v1/checks",
	ReadAccess: UserAccess{},
	GET:        v1GetChecks,
}, {
	Path:        "/v1/notices",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{},
	GET:         v1GetNotices,
	POST:        v1PostNotices,
}, {
	Path:       "/v1/notices/{id}",
	ReadAccess: UserAccess{},
	GET:        v1GetNotice,
}}

var (
	stateOkayWarnings    = (*state.State).OkayWarnings
	stateAllWarnings     = (*state.State).AllWarnings
	statePendingWarnings = (*state.State).PendingWarnings
	stateEnsureBefore    = (*state.State).EnsureBefore

	overlordServiceManager = (*overlord.Overlord).ServiceManager
	overlordPlanManager    = (*overlord.Overlord).PlanManager

	muxVars = mux.Vars
)

func v1SystemInfo(c *Command, r *http.Request, _ *UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	result := map[string]interface{}{
		"version": c.d.Version,
		"boot-id": restart.BootID(state),
	}
	return SyncResponse(result)
}
