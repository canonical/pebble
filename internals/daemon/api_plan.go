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
	"net/http"

	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/plan"
)

func v1GetPlan(c *Command, r *http.Request, _ *UserState) Response {
	format := r.URL.Query().Get("format")
	if format != "yaml" {
		return statusBadRequest("invalid format %q", format)
	}

	debug := false
	debugStr := r.URL.Query().Get("debug")
	switch debugStr {
	case "true":
		debug = true
	case "", "false":
		debug = false
	default:
		return statusBadRequest("invalid debug value %q", debugStr)
	}

	servmgr := overlordServiceManager(c.d.overlord)
	p, err := servmgr.Plan()
	if err != nil {
		return statusInternalError("%v", err)
	}
	planYAML, err := yaml.Marshal(p)
	if err != nil {
		return statusInternalError("cannot serialize plan: %v", err)
	}
	if !debug || len(p.Layers) == 0 {
		return SyncResponse(string(planYAML))
	}

	py := plan.PlanYaml{}
	err = yaml.Unmarshal(planYAML, &py)
	if err != nil {
		return statusInternalError("cannot deserialize plan yaml: %v", err)
	}

	for _, layer := range p.Layers {
		for k, svc := range layer.Services {
			sy := py.Services[k]
			if svc.Summary != "" {
				sy.Summary.LineComment = layer.Label
			}
			if svc.Description != "" {
				sy.Description.LineComment = layer.Label
			}
			if svc.Startup != plan.StartupUnknown {
				sy.Startup.LineComment = layer.Label
			}
			if svc.Command != "" {
				sy.Command.LineComment = layer.Label
			}

			// Service dependencies
			if len(svc.After) > 0 {
				for _, w := range svc.After {
					for k, vy := range sy.After {
						if vy.Value == w {
							vy.LineComment = layer.Label
							sy.After[k] = vy
						}
					}
				}
			}
			if len(svc.Before) > 0 {
				for _, w := range svc.Before {
					for k, vy := range sy.Before {
						if vy.Value == w {
							vy.LineComment = layer.Label
							sy.Before[k] = vy
						}
					}
				}
			}
			if len(svc.Requires) > 0 {
				for _, w := range svc.Requires {
					for k, vy := range sy.Requires {
						if vy.Value == w {
							vy.LineComment = layer.Label
							sy.Requires[k] = vy
						}
					}
				}
			}

			// Options for command execution
			for k := range svc.Environment {
				vy, ok := sy.Environment[k]
				if !ok {
					continue
				}
				vy.LineComment = layer.Label
				sy.Environment[k] = vy
			}
			if svc.UserID != nil {
				sy.UserID.LineComment = layer.Label
			}
			if svc.User != "" {
				sy.User.LineComment = layer.Label
			}
			if svc.GroupID != nil {
				sy.GroupID.LineComment = layer.Label
			}
			if svc.Group != "" {
				sy.Group.LineComment = layer.Label
			}
			if svc.WorkingDir != "" {
				sy.WorkingDir.LineComment = layer.Label
			}

			// Auto-restart and backoff functionality
			if svc.OnSuccess != plan.ActionUnset {
				sy.OnSuccess.LineComment = layer.Label
			}
			if svc.OnFailure != plan.ActionUnset {
				sy.OnFailure.LineComment = layer.Label
			}
			for k := range svc.OnCheckFailure {
				vy, ok := sy.OnCheckFailure[k]
				if !ok {
					continue
				}
				vy.LineComment = layer.Label
				sy.OnCheckFailure[k] = vy
			}
			if svc.BackoffDelay.IsSet {
				sy.BackoffDelay.LineComment = layer.Label
			}
			if svc.BackoffFactor.IsSet {
				sy.BackoffFactor.LineComment = layer.Label
			}
			if svc.BackoffLimit.IsSet {
				sy.BackoffLimit.LineComment = layer.Label
			}
			if svc.KillDelay.IsSet {
				sy.KillDelay.LineComment = layer.Label
			}
		}
	}

	// TODO: handle checks

	// TODO: handle log targets

	planYAML, err = yaml.Marshal(py)
	if err != nil {
		return statusInternalError("cannot serialize plan: %v", err)
	}

	return SyncResponse(string(planYAML))
}

func v1PostLayers(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Action  string `json:"action"`
		Combine bool   `json:"combine"`
		Label   string `json:"label"`
		Format  string `json:"format"`
		Layer   string `json:"layer"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request body: %v", err)
	}

	if payload.Action != "add" {
		return statusBadRequest("invalid action %q", payload.Action)
	}
	if payload.Label == "" {
		return statusBadRequest("label must be set")
	}
	if payload.Format != "yaml" {
		return statusBadRequest("invalid format %q", payload.Format)
	}
	layer, err := plan.ParseLayer(0, payload.Label, []byte(payload.Layer))
	if err != nil {
		return statusBadRequest("cannot parse layer YAML: %v", err)
	}

	servmgr := overlordServiceManager(c.d.overlord)
	if payload.Combine {
		err = servmgr.CombineLayer(layer)
	} else {
		err = servmgr.AppendLayer(layer)
	}
	if err != nil {
		if _, ok := err.(*servstate.LabelExists); ok {
			return statusBadRequest("%v", err)
		}
		if _, ok := err.(*plan.FormatError); ok {
			return statusBadRequest("%v", err)
		}
		return statusInternalError("%v", err)
	}
	return SyncResponse(true)
}
