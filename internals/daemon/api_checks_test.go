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
