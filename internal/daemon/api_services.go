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
	"sort"
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
	names := strutil.MultiCommaSeparatedList(r.URL.Query()["names"])

	servmgr := overlordServiceManager(c.d.overlord)
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
	servmgr := overlordServiceManager(c.d.overlord)
	switch payload.Action {
	case "replan":
		if len(payload.Services) != 0 {
			return statusBadRequest("%s accepts no service names", payload.Action)
		}
	case "autostart":
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
	default:
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
		services = intersectOrdered(payload.Services, services)
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
	case "replan":
		var stopNames, startNames []string
		stopNames, startNames, err = servmgr.Replan()
		if err != nil {
			break
		}
		var stopTasks *state.TaskSet
		stopTasks, err = servstate.Stop(st, stopNames)
		if err != nil {
			break
		}
		var startTasks *state.TaskSet
		startTasks, err = servstate.Start(st, startNames)
		if err != nil {
			break
		}
		startTasks.WaitAll(stopTasks)
		taskSet = state.NewTaskSet()
		taskSet.AddAll(stopTasks)
		taskSet.AddAll(startTasks)

		// Populate a list of services affected by the replan for summary.
		replanned := make(map[string]bool)
		for _, v := range stopNames {
			replanned[v] = true
		}
		for _, v := range startNames {
			replanned[v] = true
		}
		for k := range replanned {
			services = append(services, k)
		}
		sort.Strings(services)
		payload.Services = services
	default:
		return statusBadRequest("action %q is unsupported", payload.Action)
	}
	if err != nil {
		return statusBadRequest("cannot %s services: %v", payload.Action, err)
	}

	// Use the original requested service name for the summary, not the
	// resolved one. But do use the resolved set for the count.
	var summary string
	switch {
	case len(taskSet.Tasks()) == 0:
		// Can happen with a replan that has no services to stop/start. A
		// change with no tasks needs to be marked Done manually (normally a
		// change is marked Done when its last task is finished).
		summary = fmt.Sprintf("%s - no services", strings.Title(payload.Action))
		change := st.NewChange(payload.Action, summary)
		change.SetStatus(state.DoneStatus)
		return AsyncResponse(nil, change.ID())
	case len(services) == 1:
		summary = fmt.Sprintf("%s service %q", strings.Title(payload.Action), payload.Services[0])
	default:
		summary = fmt.Sprintf("%s service %q and %d more", strings.Title(payload.Action), payload.Services[0], len(services)-1)
	}

	change := st.NewChange(payload.Action, summary)
	change.AddAll(taskSet)
	if len(payload.Services) > 0 {
		change.Set("service-names", payload.Services)
	}

	stateEnsureBefore(st, 0)

	return AsyncResponse(nil, change.ID())
}

func v1GetService(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("not implemented")
}

func v1PostService(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("not implemented")
}

// intersectOrdered returns the intersection of left and right where
// the right's ordering is persisted in the resulting set.
func intersectOrdered(left []string, orderedRight []string) []string {
	m := map[string]bool{}
	for _, v := range left {
		m[v] = true
	}
	var out []string
	for _, v := range orderedRight {
		if m[v] {
			out = append(out, v)
		}
	}
	return out
}
