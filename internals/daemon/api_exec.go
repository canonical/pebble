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

package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord/cmdstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/overlord/truststate"
	"github.com/canonical/pebble/internals/plan"
)

type execPayload struct {
	Command        []string          `json:"command"`
	ServiceContext string            `json:"service-context"`
	Environment    map[string]string `json:"environment"`
	WorkingDir     string            `json:"working-dir"`
	Timeout        string            `json:"timeout"`
	UserID         *int              `json:"user-id"`
	User           string            `json:"user"`
	GroupID        *int              `json:"group-id"`
	Group          string            `json:"group"`
	Terminal       bool              `json:"terminal"`
	Interactive    bool              `json:"interactive"`
	SplitStderr    bool              `json:"split-stderr"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
}

func v1PostExec(c *Command, req *http.Request, user *UserState) Response {
	var payload execPayload
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}
	if len(payload.Command) < 1 {
		return BadRequest("must specify command")
	}

	timeout, err := parseOptionalDuration(payload.Timeout)
	if err != nil {
		return BadRequest("invalid timeout: %v", err)
	}

	// Check up-front that the executable exists.
	_, err = exec.LookPath(payload.Command[0])
	if err != nil {
		return BadRequest("cannot find executable %q", payload.Command[0])
	}

	p := c.d.overlord.PlanManager().Plan()
	overrides := plan.ContextOptions{
		Environment: payload.Environment,
		UserID:      payload.UserID,
		User:        payload.User,
		GroupID:     payload.GroupID,
		Group:       payload.Group,
		WorkingDir:  payload.WorkingDir,
	}
	merged, err := plan.MergeServiceContext(p, payload.ServiceContext, overrides)
	if err != nil {
		return BadRequest("%v", err)
	}

	trustContext, err := resolveExecTrustContext(c, p, payload.ServiceContext)
	if err != nil {
		return BadRequest("%v", err)
	}

	// Convert User/UserID and Group/GroupID combinations into raw uid/gid.
	uid, gid, err := osutil.NormalizeUidGid(merged.UserID, merged.GroupID, merged.User, merged.Group)
	if err != nil {
		if trustContext != nil {
			trustContext.Close()
		}
		return BadRequest("%v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	args := &cmdstate.ExecArgs{
		Command:      payload.Command,
		Environment:  merged.Environment,
		WorkingDir:   merged.WorkingDir,
		Timeout:      timeout,
		UserID:       uid,
		GroupID:      gid,
		Terminal:     payload.Terminal,
		Interactive:  payload.Interactive,
		SplitStderr:  payload.SplitStderr,
		Width:        payload.Width,
		Height:       payload.Height,
		TrustContext: trustContext,
	}
	task, metadata, err := cmdstate.Exec(st, args)
	if err != nil {
		if trustContext != nil {
			trustContext.Close()
		}
		return ServerError("cannot call exec: %v", err)
	}

	change := st.NewChange("exec", fmt.Sprintf("Execute command %q", args.Command[0]))
	taskSet := state.NewTaskSet(task)
	change.AddAll(taskSet)

	logger.SecurityWarn(logger.SecurityAuthzAdmin, userString(user)+",exec", "Executing command "+args.Command[0])

	stateEnsureBefore(st, 0) // start it right away

	result := map[string]any{
		"environment": metadata.Environment,
		"task-id":     metadata.TaskID,
		"working-dir": metadata.WorkingDir,
	}
	return AsyncResponse(result, change.ID())
}

// resolveExecTrustContext resolves the trust context configured for the
// service/workload. If a trust context is returned, it must be closed by the
// caller.
func resolveExecTrustContext(c *Command, p *plan.Plan, serviceContextName string) (*truststate.TrustContext, error) {
	if serviceContextName == "" {
		return nil, nil
	}
	service, ok := p.Services[serviceContextName]
	if !ok {
		return nil, nil
	}

	trustMgr := c.d.overlord.TrustManager()
	if trustMgr == nil {
		return nil, fmt.Errorf(
			"cannot resolve trust context %q for service context %q: no trust manager",
			service.TrustContext, serviceContextName,
		)
	}
	trustContext, err := trustMgr.TrustContext(service.TrustContext)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot resolve trust context %q for service context %q: %v",
			service.TrustContext, serviceContextName, err,
		)
	}

	return trustContext, nil
}
