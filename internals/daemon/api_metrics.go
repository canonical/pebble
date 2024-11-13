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
	"net/http"

	"github.com/canonical/pebble/internals/metrics"
)

func Metrics(c *Command, r *http.Request, _ *UserState) Response {
	return metricsResponse{}
}

// metricsResponse is a Response implementation to serve the metrics in a prometheus metrics format.
type metricsResponse struct{}

func (r metricsResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	registry := metrics.GetRegistry()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(registry.GatherMetrics()))

}
