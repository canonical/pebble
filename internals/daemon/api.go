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
	"strconv"

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
	Path:       "/v1/changes",
	ReadAccess: UserAccess{},
	GET:        v1GetChanges,
}, {
	Path:        "/v1/changes/{id}",
	ReadAccess:  UserAccess{},
	WriteAccess: AdminAccess{},
	GET:         v1GetChange,
	POST:        v1PostChange,
}, {
	Path:       "/v1/changes/{id}/wait",
	ReadAccess: UserAccess{},
	GET:        v1GetChangeWait,
}, {
	Path:        "/v1/services",
	ReadAccess:  UserAccess{},
	WriteAccess: AdminAccess{},
	GET:         v1GetServices,
	POST:        v1PostServices,
}, {
	Path:        "/v1/services/{name}",
	ReadAccess:  UserAccess{},
	WriteAccess: AdminAccess{},
	GET:         v1GetService,
	POST:        v1PostService,
}, {
	Path:       "/v1/plan",
	ReadAccess: UserAccess{},
	GET:        v1GetPlan,
}, {
	Path:        "/v1/layers",
	WriteAccess: AdminAccess{},
	POST:        v1PostLayers,
}, {
	Path:        "/v1/files",
	ReadAccess:  AdminAccess{}, // some files are sensitive, so require admin
	WriteAccess: AdminAccess{},
	GET:         v1GetFiles,
	POST:        v1PostFiles,
}, {
	Path:       "/v1/logs",
	ReadAccess: UserAccess{},
	GET:        v1GetLogs,
}, {
	Path:        "/v1/exec",
	WriteAccess: AdminAccess{},
	POST:        v1PostExec,
}, {
	Path:       "/v1/tasks/{task-id}/websocket/{websocket-id}",
	ReadAccess: AdminAccess{}, // used by exec, so require admin
	GET:        v1GetTaskWebsocket,
}, {
	Path:        "/v1/signals",
	WriteAccess: AdminAccess{},
	POST:        v1PostSignals,
}, {
	Path:        "/v1/checks",
	ReadAccess:  UserAccess{},
	WriteAccess: AdminAccess{},
	GET:         v1GetChecks,
	POST:        v1PostChecks,
}, {
	Path:        "/v1/checks/refresh",
	WriteAccess: AdminAccess{},
	POST:        v1PostChecksRefresh,
}, {
	Path:        "/v1/notices",
	ReadAccess:  UserAccess{},
	WriteAccess: UserAccess{}, // any user is allowed to add a notice with their own uid
	GET:         v1GetNotices,
	POST:        v1PostNotices,
}, {
	Path:       "/v1/notices/{id}",
	ReadAccess: UserAccess{},
	GET:        v1GetNotice,
}, {
	Path:        "/v1/identities",
	ReadAccess:  UserAccess{},
	WriteAccess: AdminAccess{},
	GET:         v1GetIdentities,
	POST:        v1PostIdentities,
}, {
	Path:       "/v1/metrics",
	ReadAccess: MetricsAccess{},
	GET:        v1GetMetrics,
}}

var (
	stateEnsureBefore = (*state.State).EnsureBefore

	overlordServiceManager = (*overlord.Overlord).ServiceManager
	overlordPlanManager    = (*overlord.Overlord).PlanManager
	overlordCheckManager   = (*overlord.Overlord).CheckManager

	muxVars = mux.Vars
)

func v1SystemInfo(c *Command, r *http.Request, _ *UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	result := struct {
		BootID       string `json:"boot-id"`
		HTTPAddress  string `json:"http-address,omitempty"`
		HTTPSAddress string `json:"https-address,omitempty"`
		Version      string `json:"version"`
	}{
		BootID:       restart.BootID(state),
		HTTPAddress:  c.d.options.HTTPAddress,
		HTTPSAddress: c.d.options.HTTPSAddress,
		Version:      c.d.Version,
	}
	return SyncResponse(result)
}

func userString(u *UserState) string {
	if u == nil {
		return "<unknown>"
	}
	if u.Username != "" {
		return u.Username
	}
	if u.UID != nil {
		return strconv.Itoa(int(*u.UID))
	}
	return "<unknown>"
}
