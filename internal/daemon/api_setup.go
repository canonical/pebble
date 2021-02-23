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
)

func v1GetLayer(c *Command, r *http.Request, x *userState) Response {
	servmgr := c.d.overlord.ServiceManager()
	layer, err := servmgr.FlattenedSetup()
	if err != nil {
		return statusInternalError("cannot fetch flattened layer: %v", err)
	}
	return SyncResponse(string(layer))
}

func v1PostLayer(c *Command, r *http.Request, x *userState) Response {
	var payload struct {
		Layer string `json:"layer"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode data from request body: %v", err)
	}

	servmgr := c.d.overlord.ServiceManager()
	order, err := servmgr.AddSetupLayer([]byte(payload.Layer))
	if err != nil {
		return statusInternalError("cannot add layer: %v", err)
	}

	return SyncResponse(order)
}
