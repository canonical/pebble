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
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
)

func (s *apiSuite) TestMetrics(c *C) {
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: sleep 10
`)
	d := s.daemon(c)
	d.overlord.Loop()

	// Start test service.
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	// Wait for it to be running.
	serviceMgr := d.overlord.ServiceManager()
	for i := 0; ; i++ {
		if i > 50 {
			c.Fatalf("timed out waiting for service to start")
		}
		services, err := serviceMgr.Services([]string{"test1"})
		c.Assert(err, IsNil)
		if len(services) == 1 && services[0].Current == servstate.StatusActive {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Get metrics.
	metricsCmd := apiCmd("/v1/metrics")
	metricsReq, err := http.NewRequest("GET", "/v1/metrics", nil)
	c.Assert(err, IsNil)
	metricsRec := httptest.NewRecorder()
	metricsRsp := v1GetMetrics(metricsCmd, metricsReq, nil).(metricsResponse)
	metricsRsp.ServeHTTP(metricsRec, metricsReq)
	c.Check(metricsRec.Code, Equals, 200)
	expected := `
# HELP pebble_service_start_count Number of times the service has started
# TYPE pebble_service_start_count counter
pebble_service_start_count{service="test1"} 1
# HELP pebble_service_active Whether the service is currently active (1) or not (0)
# TYPE pebble_service_active gauge
pebble_service_active{service="test1"} 1
`[1:]
	c.Assert(metricsRec.Body.String(), Equals, expected)
}
