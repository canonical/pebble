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
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	s.startOverlord()

	// Request with no filters.
	start := time.Now()
	for {
		// Health checks are started asynchronously as changes, so wait for
		// them to appear.
		rsp, body := s.getChecks(c, "")
		c.Check(rsp.Status, Equals, 200)
		c.Check(rsp.Type, Equals, ResponseTypeSync)
		expected := []any{
			map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
			map[string]any{"name": "chk2", "startup": "enabled", "status": "up", "level": "alive", "threshold": 3.0, "change-id": "C1"},
			map[string]any{"name": "chk3", "startup": "enabled", "status": "up", "threshold": 3.0, "change-id": "C2"},
		}
		if reflect.DeepEqual(body["result"], expected) {
			break
		}
		if time.Since(start) > time.Second {
			c.Fatalf("timed out waiting for checks to settle\nobtained = #%v\nexpected = %#v",
				body["result"], expected)
		}
		time.Sleep(time.Millisecond)
	}

	// Request with names filter
	rsp, body := s.getChecks(c, "?names=chk1&names=chk3")
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(body["result"], DeepEquals, []any{
		map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
		map[string]any{"name": "chk3", "startup": "enabled", "status": "up", "threshold": 3.0, "change-id": "C1"},
	})

	// Request with names filter (comma-separated values)
	rsp, body = s.getChecks(c, "?names=chk1,chk3")
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(body["result"], DeepEquals, []any{
		map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
		map[string]any{"name": "chk3", "startup": "enabled", "status": "up", "threshold": 3.0, "change-id": "C1"},
	})

	// Request with level filter
	rsp, body = s.getChecks(c, "?level=alive")
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(body["result"], DeepEquals, []any{
		map[string]any{"name": "chk2", "startup": "enabled", "status": "up", "level": "alive", "threshold": 3.0, "change-id": "C0"},
	})

	// Request with names and level filters
	rsp, body = s.getChecks(c, "?level=ready&names=chk1")
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(body["result"], DeepEquals, []any{
		map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
	})
}

func (s *apiSuite) TestChecksGetInvalidLevel(c *C) {
	s.daemon(c)
	s.startOverlord()

	rsp, body := s.getChecks(c, "?level=foo")
	c.Check(rsp.Status, Equals, 400)
	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Result, NotNil)
	c.Check(body["result"], DeepEquals, map[string]any{
		"message": `level must be "alive" or "ready"`,
	})
}

func (s *apiSuite) TestChecksEmpty(c *C) {
	s.daemon(c)
	s.startOverlord()

	rsp, body := s.getChecks(c, "")
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(body["result"], DeepEquals, []any{}) // should be [] rather than null
}

func (s *apiSuite) getChecks(c *C, query string) (*resp, map[string]any) {
	req, err := http.NewRequest("GET", "/v1/checks"+query, nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, rsp.Status)
	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)

	// Standardise the change-id fields before comparison as these can vary.
	if results, ok := body["result"].([]any); ok {
		for i, result := range results {
			resultMap := result.(map[string]any)
			c.Check(resultMap["change-id"].(string), Not(Equals), "")
			resultMap["change-id"] = fmt.Sprintf("C%d", i)
		}
	}

	return rsp, body
}
