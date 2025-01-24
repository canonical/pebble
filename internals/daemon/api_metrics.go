// Copyright (c) 2024 Canonical Ltd
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
	"bytes"
	"net/http"

	"github.com/canonical/pebble/internals/overlord/servstate"
)

func v1GetMetrics(c *Command, r *http.Request, _ *UserState) Response {
	return metricsResponse{
		svcMgr: overlordServiceManager(c.d.overlord),
	}
}

// metricsResponse is a Response implementation to serve the metrics in the OpenMetrics format.
type metricsResponse struct {
	svcMgr *servstate.ServiceManager
}

func (r metricsResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var buffer bytes.Buffer
	err := r.svcMgr.Metrics(&buffer)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(buffer.String()))
}
