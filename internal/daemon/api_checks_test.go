// Copyright (c) 2022 Canonical Ltd
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
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"
)

func (s *apiSuite) TestChecksGet(c *C) {
	writeTestLayer(s.pebbleDir, `
checks:
    chk1:
        override: replace
        level: ready
        http:
            url: https://example.com/bad

    chk2:
        override: replace
        level: alive
        tcp:
            port: 8080

    chk3:
        override: replace
        exec:
            command: sleep x
`)
	s.daemon(c)
	_, err := s.d.overlord.ServiceManager().Plan() // ensure plan is loaded
	c.Assert(err, IsNil)

	// Request with no filters
	req, err := http.NewRequest("GET", "/v1/checks", nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk1", "status": "up", "level": "ready", "threshold": 3.0},
		map[string]interface{}{"name": "chk2", "status": "up", "level": "alive", "threshold": 3.0},
		map[string]interface{}{"name": "chk3", "status": "up", "threshold": 3.0},
	})

	// Request with names filter
	req, err = http.NewRequest("GET", "/v1/checks?names=chk1&names=chk3", nil)
	c.Assert(err, IsNil)
	rsp = v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk1", "status": "up", "level": "ready", "threshold": 3.0},
		map[string]interface{}{"name": "chk3", "status": "up", "threshold": 3.0},
	})

	// Request with names filter (comma-separated values)
	req, err = http.NewRequest("GET", "/v1/checks?names=chk1,chk3", nil)
	c.Assert(err, IsNil)
	rsp = v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk1", "status": "up", "level": "ready", "threshold": 3.0},
		map[string]interface{}{"name": "chk3", "status": "up", "threshold": 3.0},
	})

	// Request with level filter
	req, err = http.NewRequest("GET", "/v1/checks?level=alive", nil)
	c.Assert(err, IsNil)
	rsp = v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk2", "status": "up", "level": "alive", "threshold": 3.0},
	})

	// Request with names and level filters
	req, err = http.NewRequest("GET", "/v1/checks?level=ready&names=chk1", nil)
	c.Assert(err, IsNil)
	rsp = v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk1", "status": "up", "level": "ready", "threshold": 3.0},
	})
}

func (s *apiSuite) TestChecksGetInvalidLevel(c *C) {
	s.daemon(c)
	_, err := s.d.overlord.ServiceManager().Plan() // ensure plan is loaded
	c.Assert(err, IsNil)

	req, err := http.NewRequest("GET", "/v1/checks?level=foo", nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rsp.Status, Equals, 400)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Result, NotNil)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, map[string]interface{}{
		"message": `level must be "alive" or "ready"`,
	})
}

func (s *apiSuite) TestChecksEmpty(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/checks", nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{}) // should be [] rather than null
}

func (s *apiSuite) TestChecksFailure(c *C) {
	writeTestLayer(s.pebbleDir, `
checks:
    chk1:
        override: replace
        period: 5ms
        threshold: 1
        exec:
            command: /bin/sh -c 'echo An error; exit 42'
`)
	s.daemon(c)
	_, err := s.d.overlord.ServiceManager().Plan() // ensure plan is loaded
	c.Assert(err, IsNil)

	// Wait for check to fail (at least once).
	for i := 0; ; i++ {
		if i >= 50 {
			c.Fatal("timeout waiting for check to fail")
		}
		checks, err := s.d.overlord.CheckManager().Checks()
		c.Assert(err, IsNil)
		c.Assert(checks, HasLen, 1)
		if checks[0].Failures > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Check the API response, including last failure
	req, err := http.NewRequest("GET", "/v1/checks", nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	checks, ok := body["result"].([]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(checks, HasLen, 1)
	check, ok := checks[0].(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Check(check, HasLen, 5)
	c.Check(check["name"], Equals, "chk1")
	c.Check(check["status"], Equals, "down")
	c.Check(check["threshold"], Equals, 1.0)
	failures, ok := check["failures"].(float64)
	c.Assert(ok, Equals, true)
	c.Check(failures >= 1, Equals, true)
	c.Check(check["last-failure"], DeepEquals, map[string]interface{}{
		"error":   "exit status 42",
		"details": "An error",
	})
}
