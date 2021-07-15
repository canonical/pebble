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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/strutil"
)

type serviceInfo struct {
	Name    string `json:"name"`
	Startup string `json:"startup"`
	Current string `json:"current"`
}

func v1GetServices(c *Command, r *http.Request, _ *userState) Response {
	names := strutil.CommaSeparatedList(r.URL.Query().Get("names"))

	servmgr := c.d.overlord.ServiceManager()
	services, err := servmgr.Services(names)
	if err != nil {
		return statusInternalError("%v", err)
	}

	infos := make([]serviceInfo, 0, len(services))
	for _, svc := range services {
		info := serviceInfo{
			Name:    svc.Name,
			Startup: string(svc.Startup),
			Current: string(svc.Current),
		}
		infos = append(infos, info)
	}
	return SyncResponse(infos)
}

func v1PostServices(c *Command, r *http.Request, _ *userState) Response {
	var payload struct {
		Action   string   `json:"action"`
		Services []string `json:"services"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode data from request body: %v", err)
	}

	var err error
	servmgr := c.d.overlord.ServiceManager()
	if payload.Action == "autostart" {
		if len(payload.Services) != 0 {
			return statusBadRequest("%s accepts no service names", payload.Action)
		}
		services, err := servmgr.DefaultServiceNames()
		if err != nil {
			return statusInternalError("%v", err)
		}
		if len(services) == 0 {
			return SyncResponse(&resp{
				Type:   ResponseTypeError,
				Result: &errorResult{Kind: errorKindNoDefaultServices, Message: "no default services"},
				Status: 400,
			})
		}
		payload.Services = services
	} else {
		if len(payload.Services) == 0 {
			return statusBadRequest("no services to %s provided", payload.Action)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var taskSet *state.TaskSet
	var services []string
	switch payload.Action {
	case "start", "autostart":
		services, err = servmgr.StartOrder(payload.Services)
		if err != nil {
			break
		}
		taskSet, err = servstate.Start(st, services)
	case "stop":
		services, err = servmgr.StopOrder(payload.Services)
		if err != nil {
			break
		}
		taskSet, err = servstate.Stop(st, services)
	case "restart":
		services, err = servmgr.StopOrder(payload.Services)
		if err != nil {
			break
		}
		var stopTasks *state.TaskSet
		stopTasks, err = servstate.Stop(st, services)
		if err != nil {
			break
		}
		services, err = servmgr.StartOrder(payload.Services)
		if err != nil {
			break
		}
		var startTasks *state.TaskSet
		startTasks, err = servstate.Start(st, services)
		if err != nil {
			break
		}
		startTasks.WaitAll(stopTasks)
		taskSet = state.NewTaskSet()
		taskSet.AddAll(stopTasks)
		taskSet.AddAll(startTasks)
	default:
		return statusBadRequest("action %q is unsupported", payload.Action)
	}
	if err != nil {
		return statusBadRequest("cannot %s services: %v", payload.Action, err)
	}

	// Use the original requested service name for the summary, not the
	// resolved one. But do use the resolved set for the count.
	var summary string
	if len(services) == 1 {
		summary = fmt.Sprintf("%s service %q", strings.Title(payload.Action), payload.Services[0])
	} else {
		summary = fmt.Sprintf("%s service %q and %d more", strings.Title(payload.Action), payload.Services[0], len(services)-1)
	}
	change := newChange(st, payload.Action, summary, []*state.TaskSet{taskSet}, payload.Services)

	stateEnsureBefore(st, 0)

	return AsyncResponse(nil, change.ID())
}

func v1GetService(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("not implemented")
}

func v1PostService(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("not implemented")
}
