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
		return BadRequest("invalid format %q", format)
	}

	servmgr := overlordServiceManager(c.d.overlord)
	plan, err := servmgr.Plan()
	if err != nil {
		return InternalError("%v", err)
	}
	planYAML, err := yaml.Marshal(plan)
	if err != nil {
		return InternalError("cannot serialize plan: %v", err)
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
		return BadRequest("cannot decode request body: %v", err)
	}

	if payload.Action != "add" {
		return BadRequest("invalid action %q", payload.Action)
	}
	if payload.Label == "" {
		return BadRequest("label must be set")
	}
	if payload.Format != "yaml" {
		return BadRequest("invalid format %q", payload.Format)
	}
	layer, err := plan.ParseLayer(0, payload.Label, []byte(payload.Layer))
	if err != nil {
		return BadRequest("cannot parse layer YAML: %v", err)
	}

	servmgr := overlordServiceManager(c.d.overlord)
	if payload.Combine {
		err = servmgr.CombineLayer(layer)
	} else {
		err = servmgr.AppendLayer(layer)
	}
	if err != nil {
		if _, ok := err.(*servstate.LabelExists); ok {
			return BadRequest("%v", err)
		}
		if _, ok := err.(*plan.FormatError); ok {
			return BadRequest("%v", err)
		}
		return InternalError("%v", err)
	}
	return SyncResponse(true)
}
