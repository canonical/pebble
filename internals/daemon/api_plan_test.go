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

	"github.com/canonical/tc"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/logger"
)

var planLayer = `
summary: this is a summary
description: this is a description
services:
    static:
        override: replace
        command: echo static
`

func (s *apiSuite) TestGetPlanErrors(c *tc.C) {
	tests := []struct {
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
		c.Assert(err, tc.IsNil)
		rsp := v1GetPlan(planCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, tc.Equals, test.status)
		c.Assert(rsp.Status, tc.Equals, test.status)
		c.Assert(rsp.Type, tc.Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, tc.Matches, test.message)
	}
}

func (s *apiSuite) TestGetPlan(c *tc.C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	planCmd := apiCmd("/v1/plan")

	req, err := http.NewRequest("GET", "/v1/plan?format=yaml", nil)
	c.Assert(err, tc.IsNil)
	rsp := v1GetPlan(planCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, tc.Equals, 200)
	c.Assert(rsp.Status, tc.Equals, 200)
	c.Assert(rsp.Type, tc.Equals, ResponseTypeSync)

	expectedYAML := `
services:
    static:
        override: replace
        command: echo static
`[1:]
	c.Assert(rsp.Result.(string), tc.Equals, expectedYAML)
	c.Assert(s.planYAML(c), tc.Equals, expectedYAML)
}

func (s *apiSuite) planYAML(c *tc.C) string {
	manager := s.d.overlord.PlanManager()
	plan := manager.Plan()
	yml, err := yaml.Marshal(plan)
	c.Assert(err, tc.IsNil)
	return string(yml)
}

func (s *apiSuite) planLayersHasLen(c *tc.C, expectedLen int) {
	manager := s.d.overlord.PlanManager()
	plan := manager.Plan()
	c.Assert(plan.Layers, tc.HasLen, expectedLen)
}

func (s *apiSuite) TestLayersErrors(c *tc.C) {
	tests := []struct {
		payload string
		status  int
		message string
	}{
		{"@", 400, `cannot decode request body: invalid character '@' looking for beginning of value`},
		{`{"action": "sub", "label": "x", "format": "yaml"}`, 400, `invalid action "sub"`},
		{`{"action": "add", "label": "", "format": "yaml"}`, 400, `label must be set`},
		{`{"action": "add", "label": "x", "format": "xml"}`, 400, `invalid format "xml"`},
		{`{"action": "add", "label": "x", "format": "yaml", "layer": "@"}`, 400, `cannot parse layer YAML: .*`},
	}

	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	for _, test := range tests {
		req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(test.payload))
		c.Assert(err, tc.IsNil)
		rsp := v1PostLayers(layersCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, tc.Equals, test.status)
		c.Assert(rsp.Status, tc.Equals, test.status)
		c.Assert(rsp.Type, tc.Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, tc.Matches, test.message)
	}
}

func (s *apiSuite) TestLayersAddAppend(c *tc.C) {
	logBuf, restore := logger.MockLogger("")
	defer restore()

	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "add", "label": "foo", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, tc.IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, tc.Equals, 200)
	c.Assert(rsp.Status, tc.Equals, 200)
	c.Assert(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), tc.Equals, true)
	c.Assert(s.planYAML(c), tc.Equals, `
services:
    dynamic:
        override: replace
        command: echo dynamic
    static:
        override: replace
        command: echo static
`[1:])
	s.planLayersHasLen(c, 2)

	ensureSecurityLog(c, logBuf.String(), "WARN", "authz_admin:<unknown>,add_layer", "Adding layer foo")
}

func (s *apiSuite) TestLayersAddCombine(c *tc.C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, tc.IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, tc.Equals, 200)
	c.Assert(rsp.Status, tc.Equals, 200)
	c.Assert(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), tc.Equals, true)
	c.Assert(s.planYAML(c), tc.Equals, `
services:
    dynamic:
        override: replace
        command: echo dynamic
    static:
        override: replace
        command: echo static
`[1:])
	s.planLayersHasLen(c, 1)
}

func (s *apiSuite) TestLayersCombineFormatError(c *tc.C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "services:\n dynamic:\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, tc.IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, tc.Equals, http.StatusBadRequest)
	c.Assert(rsp.Status, tc.Equals, http.StatusBadRequest)
	c.Assert(rsp.Type, tc.Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	c.Assert(result.Message, tc.Matches, `layer "base" must define "override" for service "dynamic"`)
}
