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

var planLayer = `
summary: this is a summary
description: this is a description
services:
    static:
        override: replace
        command: echo static
`

func (s *apiSuite) TestGetPlanErrors(c *C) {
	var tests = []struct {
		url     string
		status  int
		message string
	}{
		{"/v1/layers", 400, `invalid format ""`},
		{"/v1/layers?format=foo", 400, `invalid format "foo"`},
	}

	_ = s.daemon(c)
	planCmd := apiCmd("/v1/plan")

	for _, test := range tests {
		req, err := http.NewRequest("POST", test.url, nil)
		c.Assert(err, IsNil)
		rsp := v1GetPlan(planCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, Equals, test.status)
		c.Assert(rsp.Status, Equals, test.status)
		c.Assert(rsp.Type, Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, Matches, test.message)
	}
}

func (s *apiSuite) TestGetPlan(c *C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	planCmd := apiCmd("/v1/plan")

	req, err := http.NewRequest("GET", "/v1/plan?format=yaml", nil)
	c.Assert(err, IsNil)
	rsp := v1GetPlan(planCmd, req, nil).(*resp)
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

func (s *apiSuite) TestLayersErrors(c *C) {
	var tests = []struct {
		payload string
		status  int
		message string
	}{
		{"@", 400, `cannot decode request body: invalid character '@' looking for beginning of value`},
		{`{"action": "combine", "format": "foo"}`, 400, `invalid format "foo"`},
		{`{"action": "bar", "format": "yaml"}`, 400, `invalid action "bar"`},
	}

	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	for _, test := range tests {
		req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(test.payload))
		c.Assert(err, IsNil)
		rsp := v1PostLayers(layersCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, Equals, test.status)
		c.Assert(rsp.Status, Equals, test.status)
		c.Assert(rsp.Type, Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, Matches, test.message)
	}
}

func (s *apiSuite) TestLayersCombine(c *C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "combine", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), Equals, true)
}
