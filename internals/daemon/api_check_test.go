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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"time"

	. "gopkg.in/check.v1"
)

func (s *apiSuite) TestCheckGet(c *C) {
	writeTestLayer(s.pebbleDir, `
checks:
    chk1:
        override: replace
        level: ready
        http:
            url: https://example.com/bad
`)
	s.daemon(c)
	s.startOverlord()

	start := time.Now()
	for {
		// Health checks are started asynchronously as changes, so wait for
		// them to appear.
		rsp, body := s.getCheck(c, "?name=chk1")
		c.Check(rsp.Status, Equals, 200)
		c.Check(rsp.Type, Equals, ResponseTypeSync)
		expected := map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C"}
		if reflect.DeepEqual(body["result"], expected) {
			break
		}
		if time.Since(start) > time.Second {
			c.Fatalf("timed out waiting for checks to settle\nobtained = #%v\nexpected = %#v",
				body["result"], expected)
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *apiSuite) TestCheckNotFound(c *C) {
	s.daemon(c)
	s.startOverlord()

	rsp, _ := s.getCheck(c, "foo")
	c.Check(rsp.Status, Equals, 404)
	c.Check(rsp.Type, Equals, ResponseTypeError)
}

func (s *apiSuite) getCheck(c *C, query string) (*resp, map[string]any) {
	req, err := http.NewRequest("GET", "/v1/check"+query, nil)
	c.Assert(err, IsNil)
	rsp := v1GetCheck(apiCmd("/v1/check"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, rsp.Status)
	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)

	// Standardise the change-id fields before comparison as these can vary.
	if result, ok := body["result"].(any); ok {
		resultMap := result.(map[string]any)
		// If the change-id is not empty or nil, replace it with a fixed value.
		if changeID, ok := resultMap["change-id"].(string); ok && changeID != "" {
			resultMap["change-id"] = "C"
		} else {
			resultMap["change-id"] = ""
		}
	}

	return rsp, body
}
