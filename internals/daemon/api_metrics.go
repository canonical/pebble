// Copyright (c) 2025 Canonical Ltd
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

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/metrics"
)

func v1GetMetrics(c *Command, r *http.Request, _ *UserState) Response {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var buf bytes.Buffer
		metricsWriter := metrics.NewOpenTelemetryWriter(&buf)

		svcMgr := overlordServiceManager(c.d.overlord)
		chkMgr := overlordCheckManager(c.d.overlord)

		err := svcMgr.WriteMetrics(metricsWriter)
		if err != nil {
			logger.Noticef("Cannot write service metrics: %v", err)
			http.Error(w, "# internal server error", http.StatusInternalServerError)
			return
		}

		err = chkMgr.WriteMetrics(metricsWriter)
		if err != nil {
			logger.Noticef("Cannot write check metrics: %v", err)
			http.Error(w, "# internal server error", http.StatusInternalServerError)
			return
		}

		_, err = buf.WriteTo(w)
		if err != nil {
			logger.Noticef("Cannot write to HTTP response: %v", err)
			http.Error(w, "# internal server error", http.StatusInternalServerError)
			return
		}
	})
}
