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
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
)

func (s *apiSuite) TestChecksGet(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, `
checks:
    chk1:
        override: replace
        http:
            url: https://example.com/bad

    chk2:
        override: replace
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

	// Execute
	req, err := http.NewRequest("GET", "/v1/checks", nil)
	c.Assert(err, IsNil)
	rsp := v1GetChecks(apiCmd("/v1/checks"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Result, NotNil)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"name": "chk1", "healthy": true},
		map[string]interface{}{"name": "chk2", "healthy": true},
		map[string]interface{}{"name": "chk3", "healthy": true},
	})
}
