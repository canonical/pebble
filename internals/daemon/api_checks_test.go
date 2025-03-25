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
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
			// If the change-id is not empty or nil, replace it with a fixed value.
			if changeID, ok := resultMap["change-id"].(string); ok && changeID != "" {
				resultMap["change-id"] = fmt.Sprintf("C%d", i)
			} else {
				resultMap["change-id"] = ""
			}
		}
	}

	return rsp, body
}

func (s *apiSuite) TestChecksPost(c *C) {
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
        startup: disabled
        tcp:
            port: 8080

    chk3:
        override: replace
        startup: enabled
        exec:
            command: sleep x
`)
	s.daemon(c)
	s.startOverlord()

	start := time.Now()
	for {
		// Health checks are started asynchronously as changes, so wait for
		// them to appear.
		rsp, body := s.getChecks(c, "")
		c.Check(rsp.Status, Equals, 200)
		c.Check(rsp.Type, Equals, ResponseTypeSync)
		expected := []any{
			map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
			map[string]any{"name": "chk2", "startup": "disabled", "status": "inactive", "level": "alive", "threshold": 3.0, "change-id": ""},
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

	rsp := s.postChecks(c, `{"action": "stop", "checks": ["chk2", "chk3", "chk1"]}`)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	// chk1 and chk3 will have stopped, and the response will list them
	// alphabetically, not in the order we provided.
	c.Check(rsp.Result.(responsePayload).Changed, DeepEquals, []string{"chk1", "chk3"})
}

func (s *apiSuite) postChecks(c *C, body string) *resp {
	req, err := http.NewRequest("POST", "/v1/checks", strings.NewReader(body))
	c.Assert(err, IsNil)
	rsp := v1PostChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, rsp.Status)
	return rsp
}

func (s *apiSuite) TestPostChecksRefresh(c *C) {
	checksYAML := `
checks:
    chk1:
        override: replace
        level: ready
        exec:
            command: /bin/sh -c "{{.CheckCommand}}"
`
	tempDir := c.MkDir()
	donePath := filepath.Join(tempDir, "doneCheck")
	checkCommand := fmt.Sprintf("sync; touch %s", donePath)
	checksYAML = strings.Replace(checksYAML, "{{.CheckCommand}}", checkCommand, -1)
	writeTestLayer(s.pebbleDir, checksYAML)
	s.daemon(c)
	s.startOverlord()

	start := time.Now()
	for {
		rsp, body := s.getChecks(c, "")
		c.Check(rsp.Status, Equals, 200)
		c.Check(rsp.Type, Equals, ResponseTypeSync)
		expected := []any{
			map[string]any{"name": "chk1", "startup": "enabled", "status": "up", "level": "ready", "threshold": 3.0, "change-id": "C0"},
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

	req, err := http.NewRequest("POST", "/v1/checks/refresh", strings.NewReader(`{"name": "chk1"}`))
	c.Assert(err, IsNil)
	rsp := v1PostChecksRefresh(apiCmd("/v1/checks/refresh"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Make the sure the check has refreshed.
	stat, err := os.Stat(donePath)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().IsRegular(), Equals, true)
	os.Remove(donePath)

	c.Check(rec.Code, Equals, rsp.Status)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	info := rsp.Result.(refreshPayload).Info

	// If the change-id is not empty or nil, replace it with a fixed value.
	if info.ChangeID != "" {
		info.ChangeID = "C0"
	}

	c.Check(info, DeepEquals, checkInfo{
		Name:      "chk1",
		Level:     "ready",
		Startup:   "enabled",
		Status:    "up",
		Failures:  0,
		Threshold: 3,
		ChangeID:  "C0",
	})
	c.Check(rsp.Result.(refreshPayload).Error, Equals, "")
}
