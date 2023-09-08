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
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"
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

	expectedYAML := `
services:
    static:
        override: replace
        command: echo static
`[1:]
	c.Assert(rsp.Result.(string), Equals, expectedYAML)
	c.Assert(s.planYAML(c), Equals, expectedYAML)
}

func (s *apiSuite) planYAML(c *C) string {
	manager := s.d.overlord.ServiceManager()
	plan, err := manager.Plan()
	c.Assert(err, IsNil)
	yml, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	return string(yml)
}

func (s *apiSuite) planLayersHasLen(c *C, expectedLen int) {
	manager := s.d.overlord.ServiceManager()
	plan, err := manager.Plan()
	c.Assert(err, IsNil)
	c.Assert(plan.Layers, HasLen, expectedLen)
}

func (s *apiSuite) TestLayersErrors(c *C) {
	var tests = []struct {
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

func (s *apiSuite) TestLayersAddAppend(c *C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "add", "label": "foo", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), Equals, true)
	c.Assert(s.planYAML(c), Equals, `
services:
    dynamic:
        override: replace
        command: echo dynamic
    static:
        override: replace
        command: echo static
`[1:])
	s.planLayersHasLen(c, 2)
}

func (s *apiSuite) TestLayersAddCombine(c *C) {
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")

	payload := `{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	c.Assert(rsp.Result.(bool), Equals, true)
	c.Assert(s.planYAML(c), Equals, `
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

// ! missing override in one service
func (s *apiSuite) TestLayersCombineOneError(c *C) {
	testLayersCombineFormatError(s, c,
		"services:\n dynamic:\n  command: echo dynamic\n",
		`layer "base" must define "override" for service "dynamic"`)

}

func (s *apiSuite) TestLayersCombineServicesMultiErrors(c *C) {
	input := `
services:
	svc1:
	  override: replace
	  command: ""
	  on-success: invalid
	  on-failure: invalid
	  on-check-failure:
		 svc1: foo
	  backoff-factor: 0
	svc2:
	  override: replace
	  command: invalidCommand
	  on-success: invalid
	  on-failure: invalid
	  on-check-failure:
		 svc1: foo
	  backoff-factor: 0
	svc3:
	  override: foo
	  command: ""
	  on-success: invalid
	  on-failure: invalid
	  on-check-failure:
		 svc3: foo
	  backoff-factor: 0
	svc4:
	  command: cmd
	  on-success: invalid
	  on-failure: invalid
	  on-check-failure:
		 svc4: foo
	  backoff-factor: 0
	  `

	expectedError := `multiple errors validating plan:
- layer "base" has invalid "override" value for service "svc3"
- layer "base" must define "override" for service "svc4"
- plan service "svc2" on-success action "invalid" invalid
- plan service "svc2" on-failure action "invalid" invalid
- plan service "svc2" on-check-failure action "foo" invalid
- plan service "svc2" backoff-factor must be 1.0 or greater, not 0
- plan must define "command" for service "svc1"
- plan service "svc1" on-success action "invalid" invalid
- plan service "svc1" on-failure action "invalid" invalid
- plan service "svc1" on-check-failure action "foo" invalid
- plan service "svc1" backoff-factor must be 1.0 or greater, not 0
`

	testLayersCombineFormatError(s, c, input, expectedError)

}

func (s *apiSuite) TestLayersCombineChecksMultiErrors(c *C) {
	input := `
checks:
  svc1:
    override: replace
    level: foo
    period: 0
    timeout: 0
    http:
      url: ""
    tcp:
      port: 0
    exec:
      command: ""
      service-context: foo
      user-id: 0
  svc2:
    override: replace
    level: alive
    period: 10s
    timeout: 10s
    exec:
      command: invalid
  svc3:
    override: foo
    level: alive
    exec:
      command: ""
  svc4:
    level: alive
    exec:
      command: cmd
	  `

	expectedError := `multiple errors validating plan:
- layer "base" has invalid "override" value for check "svc3"
- layer "base" must define "override" for check "svc4"
- plan check "svc2" timeout must be less than period
- plan check "svc1" level must be "alive" or "ready"
- plan check "svc1" period must not be zero
- plan check "svc1" timeout must not be zero
- plan must set "url" for http check "svc1"
- plan must set "port" for tcp check "svc1"
- plan must set "command" for exec check "svc1"
- plan check "svc1" service context specifies non-existent service "foo"
- plan check "svc1" has invalid user/group: must specify group, not just UID
- plan must specify one of "http", "tcp", or "exec" for check "svc1"
`

	testLayersCombineFormatError(s, c, input, expectedError)

}

func (s *apiSuite) TestLayersCombineLogTargetMultiErrors(c *C) {
	input := `
log-targets:
  svc1:
    override: replace
    type: foo
  svc2:
    override: replace
    type: ""
    services: ["foo"]
  svc3:
    override: foo
    type: ""
  svc4:
    type: loki
    services: ["foo"]
	  `

	expectedError := `multiple errors validating plan:
- layer "base" has invalid "override" value for log target "svc3"
- layer "base" must define "override" for log target "svc4"
- log target "svc1" has unsupported type "foo", must be "loki" or "syslog"
- plan must define "location" for log target "svc1"
- plan must define "type" ("loki" or "syslog") for log target "svc2"
- log target "svc2" specifies unknown service "foo"
- plan must define "location" for log target "svc2"

`

	testLayersCombineFormatError(s, c, input, expectedError)

}

func testLayersCombineFormatError(s *apiSuite, c *C, content string, response string) {
	// Manually escape the string
	content = strings.ReplaceAll(content, "\t", "    ")
	escapedContent := strings.ReplaceAll(content, "\n", "\\n")
	escapedContent = strings.ReplaceAll(escapedContent, "\"", "\\\"")
	payload := fmt.Sprintf(`{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "%s"}`, escapedContent)
	writeTestLayer(s.pebbleDir, planLayer)
	_ = s.daemon(c)
	layersCmd := apiCmd("/v1/layers")
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayers(layersCmd, req, nil).(*resp)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
	c.Assert(rsp.Status, Equals, http.StatusBadRequest)
	c.Assert(rsp.Type, Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	sortedExpected := sortErrorLines(response)
	sortedActual := sortErrorLines(result.Message)
	c.Assert(sortedActual, Equals, sortedExpected)
}

func sortErrorLines(errorStr string) string {
	lines := strings.Split(errorStr, "\n")

	// Filter out empty lines
	var nonEmptyLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmptyLines = append(nonEmptyLines, trimmed)
		}
	}

	// Sort the non-empty lines
	sort.Strings(nonEmptyLines)

	return strings.Join(nonEmptyLines, "\n")
}
