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

	. "gopkg.in/check.v1"
)

var setupLayer = `
services:
    static:
        override: replace
        command: echo static
`

func (s *apiSuite) TestLayersErrors(c *C) {
	var tests = []struct {
		payload string
		status  int
		message string
	}{
		{"@", 400, `cannot decode request body: invalid character '@' looking for beginning of value`},
		{`{"action": "merge", "format": "foo"}`, 400, `invalid format "foo"`},
		{`{"action": "flatten", "format": "foo"}`, 400, `invalid format "foo"`},
		{`{"action": "bar", "format": "yaml"}`, 400, `invalid action "bar"`},
	}

	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	for _, test := range tests {
		req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(test.payload))
		c.Assert(err, IsNil)
		rsp := v1PostLayer(layersCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, Equals, test.status)
		c.Assert(rsp.Status, Equals, test.status)
		c.Assert(rsp.Type, Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, Matches, test.message)
	}
}

func (s *apiSuite) TestLayersFlatten(c *C) {
	writeTestLayer(s.pebbleDir, setupLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "flatten", "format": "yaml"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayer(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(string), Equals, `
services:
    static:
        override: replace
        command: echo static
`[1:])
}

func (s *apiSuite) TestLayersMerge(c *C) {
	writeTestLayer(s.pebbleDir, setupLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "merge", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayer(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), Equals, true)

	payload = `{"action": "flatten", "format": "yaml"}`
	req, err = http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp = v1PostLayer(layersCmd, req, nil).(*resp)
	rec = httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(string), Equals, `
services:
    dynamic:
        override: replace
        command: echo dynamic
    static:
        override: replace
        command: echo static
`[1:])
}
