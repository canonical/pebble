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
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/cmdstate"
	"github.com/canonical/pebble/internal/overlord/state"
)

type changeInfo struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Summary string      `json:"summary"`
	Status  string      `json:"status"`
	Tasks   []*taskInfo `json:"tasks,omitempty"`
	Ready   bool        `json:"ready"`
	Err     string      `json:"err,omitempty"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`

	Data map[string]*json.RawMessage `json:"data,omitempty"`
}

type taskInfo struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Summary  string           `json:"summary"`
	Status   string           `json:"status"`
	Log      []string         `json:"log,omitempty"`
	Progress taskInfoProgress `json:"progress"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`
}

type taskInfoProgress struct {
	Label string `json:"label"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func change2changeInfo(chg *state.Change) *changeInfo {
	status := chg.Status()
	chgInfo := &changeInfo{
		ID:      chg.ID(),
		Kind:    chg.Kind(),
		Summary: chg.Summary(),
		Status:  status.String(),
		Ready:   status.Ready(),

		SpawnTime: chg.SpawnTime(),
	}
	readyTime := chg.ReadyTime()
	if !readyTime.IsZero() {
		chgInfo.ReadyTime = &readyTime
	}
	if err := chg.Err(); err != nil {
		chgInfo.Err = err.Error()
	}

	tasks := chg.Tasks()
	taskInfos := make([]*taskInfo, len(tasks))
	for j, t := range tasks {
		label, done, total := t.Progress()

		taskInfo := &taskInfo{
			ID:      t.ID(),
			Kind:    t.Kind(),
			Summary: t.Summary(),
			Status:  t.Status().String(),
			Log:     t.Log(),
			Progress: taskInfoProgress{
				Label: label,
				Done:  done,
				Total: total,
			},
			SpawnTime: t.SpawnTime(),
		}
		readyTime := t.ReadyTime()
		if !readyTime.IsZero() {
			taskInfo.ReadyTime = &readyTime
		}
		taskInfos[j] = taskInfo
	}
	chgInfo.Tasks = taskInfos

	var data map[string]*json.RawMessage
	if chg.Get("api-data", &data) == nil {
		chgInfo.Data = data
	}

	return chgInfo
}

func v1GetChanges(c *Command, r *http.Request, _ *userState) Response {
	query := r.URL.Query()
	qselect := query.Get("select")
	if qselect == "" {
		qselect = "in-progress"
	}
	var filter func(*state.Change) bool
	switch qselect {
	case "all":
		filter = func(*state.Change) bool { return true }
	case "in-progress":
		filter = func(chg *state.Change) bool { return !chg.Status().Ready() }
	case "ready":
		filter = func(chg *state.Change) bool { return chg.Status().Ready() }
	default:
		return statusBadRequest("select should be one of: all,in-progress,ready")
	}

	if wantedName := query.Get("for"); wantedName != "" {
		outerFilter := filter
		filter = func(chg *state.Change) bool {
			if !outerFilter(chg) {
				return false
			}

			var serviceNames []string
			if err := chg.Get("service-names", &serviceNames); err != nil {
				logger.Noticef("Cannot get service-name for change %v", chg.ID())
				return false
			}

			for _, serviceName := range serviceNames {
				if serviceName == wantedName {
					return true
				}
			}

			return false
		}
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chgs := state.Changes()
	chgInfos := make([]*changeInfo, 0, len(chgs))
	for _, chg := range chgs {
		if !filter(chg) {
			continue
		}
		chgInfos = append(chgInfos, change2changeInfo(chg))
	}
	return SyncResponse(chgInfos)
}

func v1GetChange(c *Command, r *http.Request, _ *userState) Response {
	changeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(changeID)
	if chg == nil {
		return statusNotFound("cannot find change with id %q", changeID)
	}

	return SyncResponse(change2changeInfo(chg))
}

func v1GetChangeWait(c *Command, r *http.Request, _ *userState) Response {
	changeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	change := st.Change(changeID)
	st.Unlock()
	if change == nil {
		return statusNotFound("cannot find change with id %q", changeID)
	}

	timeoutStr := r.URL.Query().Get("timeout")
	if timeoutStr != "" {
		// Timeout specified, wait till change is ready or timeout occurs,
		// whichever is first.
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return statusBadRequest("invalid timeout %q: %v", timeoutStr, err)
		}
		timer := time.NewTimer(timeout)
		select {
		case <-change.Ready():
			timer.Stop() // change ready, release timer resources
		case <-timer.C:
			return statusGatewayTimeout("timed out waiting for change after %s", timeout)
		case <-r.Context().Done():
			return statusInternalError("request cancelled")
		}
	} else {
		// No timeout, wait indefinitely for change to be ready.
		select {
		case <-change.Ready():
		case <-r.Context().Done():
			return statusInternalError("request cancelled")
		}
	}

	st.Lock()
	defer st.Unlock()
	return SyncResponse(change2changeInfo(change))
}

func v1GetChangeWebsocket(c *Command, req *http.Request, _ *userState) Response {
	changeID := muxVars(req)["id"]

	websocketID := req.URL.Query().Get("id")
	if websocketID == "" {
		return statusBadRequest("must specify id")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	change := st.Change(changeID)
	if change == nil {
		return statusNotFound("cannot find change with id %q", changeID)
	}
	connect := connectFuncs[change.Kind()]
	if connect == nil {
		return statusBadRequest("%q changes do not have websockets", change.Kind())
	}

	return websocketResponse{
		change:      change,
		websocketID: websocketID,
		connect:     connect,
	}
}

type websocketResponse struct {
	change      *state.Change
	websocketID string
	connect     connectFunc
}

func (wr websocketResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := wr.connect(r, w, wr.change, wr.websocketID)
	if errors.Is(err, os.ErrNotExist) {
		rsp := statusNotFound("cannot find websocket with id %q", wr.websocketID)
		rsp.ServeHTTP(w, r)
		return
	}
	if err != nil {
		rsp := statusInternalError("cannot connect to websocket: %v", err)
		rsp.ServeHTTP(w, r)
		return
	}
	// In the success case, Connect takes over the connection and upgrades to
	// the websocket protocol.
}

type connectFunc func(r *http.Request, w http.ResponseWriter, change *state.Change, websocketID string) error

// connectFuncs maps change kind to websocket connect function.
var connectFuncs = map[string]connectFunc{
	"exec": cmdstate.Connect,
}

func v1PostChange(c *Command, r *http.Request, _ *userState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return statusNotFound("cannot find change with id %q", chID)
	}

	var reqData struct {
		Action string `json:"action"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqData); err != nil {
		return statusBadRequest("cannot decode data from request body: %v", err)
	}

	if reqData.Action != "abort" {
		return statusBadRequest("change action %q is unsupported", reqData.Action)
	}

	if chg.Status().Ready() {
		return statusBadRequest("cannot abort change %s with nothing pending", chID)
	}

	// flag the change
	chg.Abort()

	// actually ask to proceed with the abort
	stateEnsureBefore(state, 0)

	return SyncResponse(change2changeInfo(chg))
}
