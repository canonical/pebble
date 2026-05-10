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
	"bytes"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/servstate"
)

func (s *apiSuite) TestSignalsSend(c *tc.C) {
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: sleep 10
        on-failure: ignore
`)
	d := s.daemon(c)
	s.startOverlord()

	// Start test service
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 202)

	// Wait for it to be running
	serviceMgr := d.overlord.ServiceManager()
	for i := 0; ; i++ {
		if i > 50 {
			c.Fatalf("timed out waiting for service to start")
		}
		services, err := serviceMgr.Services([]string{"test1"})
		c.Assert(err, tc.ErrorIsNil)
		if len(services) == 1 && services[0].Current == servstate.StatusActive {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// First ensure a bad signal name returns an error
	payload = bytes.NewBufferString(`{"signal": "FOOBAR", "services": ["test1"]}`)
	req, err = http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp = v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 500)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(errResult.Message, tc.Matches, `cannot send signal to "test1": invalid signal name "FOOBAR"`)

	// Send SIGTERM to service via API
	payload = bytes.NewBufferString(`{"signal": "SIGTERM", "services": ["test1"]}`)
	req, err = http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp = v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 200)
	c.Check(rsp.Result, tc.DeepEquals, true)

	// Ensure it goes into error state due to the SIGTERM signal.
	// The service returns with a non-zero exit code's return code because of SIGTERM,
	// and since on-failure is configured as ignore, the state is transitioned into
	// exited, corresponding to the error status.
	for i := 0; ; i++ {
		if i > 50 {
			c.Fatalf("timed out waiting for service to go into backoff")
		}
		services, err := serviceMgr.Services([]string{"test1"})
		c.Assert(err, tc.ErrorIsNil)
		if len(services) == 1 && services[0].Current == servstate.StatusError {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *apiSuite) TestSignalsBadBody(c *tc.C) {
	payload := bytes.NewBufferString("@")
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 400)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(errResult.Message, tc.Matches, "cannot decode request body: .*")
}

func (s *apiSuite) TestSignalsNoServices(c *tc.C) {
	payload := bytes.NewBufferString(`{"signal": "SIGTERM"}`)
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 400)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(errResult.Message, tc.Equals, "must specify one or more services")
}

func (s *apiSuite) TestSignalsServiceNotRunning(c *tc.C) {
	s.daemon(c)
	payload := bytes.NewBufferString(`{"signal": "SIGTERM", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, tc.ErrorIsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, tc.Equals, 500)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(errResult.Message, tc.Matches, `cannot send signal to "test1": service is not running`)
}
