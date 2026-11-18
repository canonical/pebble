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
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
)

func v1GetPlan(c *Command, r *http.Request, _ *UserState) Response {
	format := r.URL.Query().Get("format")
	planMgr := overlordPlanManager(c.d.overlord)
	p := planMgr.Plan()
	var data []byte
	var err error
	switch format {
	case "yaml":
		data, err = yaml.Marshal(p)
	case "cnf":
		data, err = plan.MarshalPlanCNF(p)
	default:
		return BadRequest("invalid format %q", format)
	}
	if err != nil {
		return ServerError("cannot serialize plan: %v", err)
	}
	return SyncResponse(string(data))
}

func v1PostLayers(c *Command, r *http.Request, user *UserState) Response {
	var payload struct {
		Action  string `json:"action"`
		Combine bool   `json:"combine"`
		Inner   bool   `json:"inner"`
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
	var layer *plan.Layer
	var err error
	switch payload.Format {
	case "yaml":
		layer, err = plan.ParseLayer(0, payload.Label, []byte(payload.Layer))
	case "cnf":
		layer, err = plan.ParseLayerCNF(0, payload.Label, []byte(payload.Layer))
	default:
		return BadRequest("invalid format %q", payload.Format)
	}
	if err != nil {
		return BadRequest("cannot parse layer %s: %v", strings.ToUpper(payload.Format), err)
	}

	logger.SecurityWarn(logger.SecurityAuthzAdmin, userString(user)+",add_layer", "Adding layer "+payload.Label)

	planMgr := overlordPlanManager(c.d.overlord)
	if payload.Combine {
		err = planMgr.CombineLayer(layer, payload.Inner)
	} else {
		err = planMgr.AppendLayer(layer, payload.Inner)
	}
	if err != nil {
		if _, ok := err.(*planstate.LabelExists); ok {
			return BadRequest("%v", err)
		}
		if _, ok := err.(*plan.FormatError); ok {
			return BadRequest("%v", err)
		}
		return ServerError("%v", err)
	}
	return SyncResponse(true)
}
