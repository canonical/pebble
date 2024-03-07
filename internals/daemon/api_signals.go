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

type signalsPayload struct {
	Signal   string   `json:"signal"`
	Services []string `json:"services"`
}

func v1PostSignals(c *Command, req *http.Request, _ *UserState) Response {
	var payload signalsPayload
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}
	if len(payload.Services) == 0 {
		return BadRequest("must specify one or more services")
	}

	serviceMgr := c.d.overlord.ServiceManager()
	err := serviceMgr.SendSignal(payload.Services, payload.Signal)
	if err != nil {
		return InternalError("%s", err)
	}
	return SyncResponse(true)
}
