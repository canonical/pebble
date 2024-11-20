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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
)

func (s *apiSuite) TestSignalsSend(c *C) {
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: sleep 10
        on-failure: ignore
`)
	d := s.daemon(c)
	d.overlord.Loop()

	// Start test service
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	// Wait for it to be running
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

	// First ensure a bad signal name returns an error
	payload = bytes.NewBufferString(`{"signal": "FOOBAR", "services": ["test1"]}`)
	req, err = http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, IsNil)
	rsp = v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 500)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(errResult.Message, Matches, `cannot send signal to "test1": invalid signal name "FOOBAR"`)

	// Send SIGTERM to service via API
	payload = bytes.NewBufferString(`{"signal": "SIGTERM", "services": ["test1"]}`)
	req, err = http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, IsNil)
	rsp = v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 200)
	c.Check(rsp.Result, DeepEquals, true)

	// Ensure it goes into inactive state due to the signal
	for i := 0; ; i++ {
		if i > 50 {
			c.Fatalf("timed out waiting for service to go into backoff")
		}
		services, err := serviceMgr.Services([]string{"test1"})
		c.Assert(err, IsNil)
		if len(services) == 1 && services[0].Current == servstate.StatusInactive {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *apiSuite) TestSignalsBadBody(c *C) {
	payload := bytes.NewBufferString("@")
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, IsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 400)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(errResult.Message, Matches, "cannot decode request body: .*")
}

func (s *apiSuite) TestSignalsNoServices(c *C) {
	payload := bytes.NewBufferString(`{"signal": "SIGTERM"}`)
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, IsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 400)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(errResult.Message, Equals, "must specify one or more services")
}

func (s *apiSuite) TestSignalsServiceNotRunning(c *C) {
	s.daemon(c)
	payload := bytes.NewBufferString(`{"signal": "SIGTERM", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/signals", payload)
	c.Assert(err, IsNil)
	rsp := v1PostSignals(apiCmd("/v1/signals"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 500)
	errResult, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(errResult.Message, Matches, `cannot send signal to "test1": service is not running`)
}
