// Copyright (c) 2026 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
)

func (s *S) TestPlanJSONTagsMatchYAMLTags(c *C) {
	values := []any{
		plan.Plan{},
		plan.Layer{},
		plan.Service{},
		plan.Check{},
		plan.HTTPCheck{},
		plan.TCPCheck{},
		plan.ExecCheck{},
		plan.LogTarget{},
	}
	for _, value := range values {
		typ := reflect.TypeOf(value)
		for field := range typ.Fields() {
			if field.Name == "Sections" {
				// YAML supports inline maps, while JSON handles extensions separately.
				continue
			}
			jsonTag := strings.ReplaceAll(field.Tag.Get("json"), "omitzero", "omitempty")
			c.Check(jsonTag, Equals, field.Tag.Get("yaml"),
				Commentf("%s.%s JSON and YAML tags differ", typ.Name(), field.Name))
		}
	}
}

func (s *S) TestParseLayerCNF(c *C) {
	yamlLayer, err := plan.ParseLayer(1, "example", reindent(`
		summary: Example
		description: |
			Example configuration.
		services:
			srv.one:
				override: replace
				command: cmd [ --foo "bar" ]
				startup: enabled
				after: [db]
				requires: [db]
				environment:
					BOOL: "true"
					NUMBER: "42"
					NOTHING: ""
				user-id: 1000
				backoff-delay: 1s
				backoff-factor: 1.5
			db:
				override: replace
				command: db
		checks:
			ready:
				override: replace
				level: ready
				period: 2s
				threshold: 2
				http:
					url: http://localhost/ready
					headers:
						Authorization: Basic password
		log-targets:
			logs:
				override: replace
				type: loki
				location: http://localhost:3100/loki/api/v1/push
				services: [srv.one]
				labels:
					environment: test
	`))
	c.Assert(err, IsNil)

	cnfLayer, err := plan.ParseLayerCNF(1, "example", []byte(`
summary: "Example"
description:
    '''
    Example configuration.
    '''
services:
    "srv.one":
        override: "replace"
        command: 'cmd [ --foo "bar" ]'
        startup: "enabled"
        after: ["db"]
        requires: ["db"]
        environment:
            BOOL: "true"
            NUMBER: "42"
            NOTHING: ""
        user-id: 0x3e8
        backoff-delay: "1s"
        backoff-factor: 1.5
    db:
        override: "replace"
        command: "db"
checks:
    ready:
        override: "replace"
        level: "ready"
        period: "2s"
        threshold: 2
        http:
            url: "http://localhost/ready"
            headers: {Authorization: "Basic password"}
log-targets:
    logs:
        override: "replace"
        type: "loki"
        location: "http://localhost:3100/loki/api/v1/push"
        services: ["srv.one"]
        labels: {environment: "test"}
`))
	c.Assert(err, IsNil)
	c.Assert(cnfLayer, DeepEquals, yamlLayer)
}

func (s *S) TestParseLayerCNFErrors(c *C) {
	tests := []struct {
		summary string
		data    string
		error   string
	}{{
		summary: "Invalid CNF syntax",
		data:    `summary: unquoted`,
		error:   `cannot parse layer "broken": 1:10: cannot parse unquoted value ` + "`unquoted`",
	}, {
		summary: "Unknown top-level section",
		data:    `future: true`,
		error:   `cannot parse layer "broken": unknown section "future"`,
	}, {
		summary: "Unknown built-in field",
		data:    "services:\n    srv:\n        future: true\n",
		error:   `cannot parse layer "broken" section "services": json: .*unknown object member name "future".*`,
	}, {
		summary: "Invalid duration",
		data:    "services:\n    srv:\n        backoff-delay: \"invalid\"\n",
		error:   `cannot parse layer "broken" section "services": json: .*invalid duration "invalid"`,
	}, {
		summary: "Invalid optional float",
		data:    "services:\n    srv:\n        backoff-factor: \"1.5\"\n",
		error:   `cannot parse layer "broken" section "services": json: .*value must be a JSON number`,
	}, {
		summary: "Non-string environment value",
		data:    "services:\n    srv:\n        environment: {PORT: 8080}\n",
		error:   `cannot parse layer "broken" section "services": json: cannot unmarshal JSON number into Go string within "/srv/environment/PORT"`,
	}, {
		summary: "Top-level value must be a map",
		data:    `[1, 2, 3]`,
		error:   `cannot parse layer "broken": top-level value must be a map`,
	}, {
		summary: "Layer validation still applies",
		data:    "services:\n    pebble:\n        override: \"replace\"\n        command: \"cmd\"\n",
		error:   `cannot use reserved service name "pebble"`,
	}}

	for _, test := range tests {
		c.Logf(test.summary)
		_, err := plan.ParseLayerCNF(0, "broken", []byte(test.data))
		c.Assert(err, ErrorMatches, test.error)
		_, ok := err.(*plan.FormatError)
		c.Assert(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))
	}
}

func (s *S) TestParseLayerCNFSectionExtension(c *C) {
	plan.RegisterSectionExtension(xField, &xExtension{})
	defer plan.UnregisterSectionExtension(xField)

	layer, err := plan.ParseLayerCNF(1, "extension", []byte(`
x-field:
    somepolicy: "enabled"
    x1:
        override: "replace"
        a: "a"
        b: "b"
`))
	c.Assert(err, IsNil)
	c.Assert(layer.Sections[xField], DeepEquals, &xSection{
		SomePolicy: "enabled",
		Entries: map[string]*X{
			"x1": {
				Name:     "x1",
				Override: plan.ReplaceOverride,
				A:        "a",
				B:        "b",
			},
		},
	})
}

func (s *S) TestReadDirCNF(c *C) {
	layersDir := filepath.Join(c.MkDir(), "layers")
	err := os.MkdirAll(filepath.Join(layersDir, "003-extra.d"), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(layersDir, "001-base.yaml"), reindent(`
		services:
			srv:
				override: replace
				command: base
	`), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(layersDir, "002-override.cnf"), []byte(`
services:
    srv:
        override: "merge"
        environment: {PORT: "8080"}
`), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(layersDir, "003-extra.d", "001-check.cnf"), []byte(`
checks:
    ready:
        override: "replace"
        exec: {command: "check"}
`), 0644)
	c.Assert(err, IsNil)

	p, err := plan.ReadDir(layersDir, nil)
	c.Assert(err, IsNil)
	c.Assert(len(p.Layers), Equals, 3)
	c.Assert(p.Layers[0].Label, Equals, "base")
	c.Assert(p.Layers[1].Label, Equals, "override")
	c.Assert(p.Layers[2].Label, Equals, "extra/check")
	c.Assert(p.Services["srv"].Command, Equals, "base")
	c.Assert(p.Services["srv"].Environment, DeepEquals, map[string]string{"PORT": "8080"})
	c.Assert(p.Checks["ready"].Exec.Command, Equals, "check")
}

func (s *S) TestReadLayersDirCNFDuplicateLabel(c *C) {
	tests := []struct {
		first  string
		second string
		error  string
	}{
		{
			first:  "001-foo.yaml",
			second: "002-foo.cnf",
			error:  `invalid layer filename: "002-foo.cnf" not unique \(have "001-foo.yaml" already\)`,
		},
		{
			first:  "001-foo.cnf",
			second: "002-foo.yaml",
			error:  `invalid layer filename: "002-foo.yaml" not unique \(have "001-foo.cnf" already\)`,
		},
	}

	for _, test := range tests {
		layersDir := c.MkDir()
		// Quoted strings are valid in both YAML and CNF.
		data := []byte("summary: \"example\"\n")
		err := os.WriteFile(filepath.Join(layersDir, test.first), data, 0644)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(layersDir, test.second), data, 0644)
		c.Assert(err, IsNil)

		_, err = plan.ReadLayersDir(layersDir)
		c.Assert(err, ErrorMatches, test.error)
	}
}
