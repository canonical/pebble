//
// Copyright (c) 2020 Canonical Ltd
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

package setup_test

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/canonical/pebble/internal/setup"

	. "gopkg.in/check.v1"
)

// TODOs:
// - command-chain
// - error on invalid keys
// - constraints on service names

// The YAML on tests below passes throught this function to deindent and
// replace tabs by spaces, so we can keep the code here sane.
func reindent(in string) []byte {
	var buf bytes.Buffer
	var trim string
	for _, line := range strings.Split(in, "\n") {
		if trim == "" {
			trimmed := strings.TrimLeft(line, "\t")
			if trimmed == "" {
				continue
			}
			if trimmed[0] == ' ' {
				panic("Tabs and spaces mixed early on string:\n" + in)
			}
			trim = line[:len(line)-len(trimmed)]
		}
		trimmed := strings.TrimPrefix(line, trim)
		if len(trimmed) == len(line) && strings.Trim(line, "\t ") != "" {
			panic("Line not indented consistently:\n" + line)
		}
		trimmed = strings.ReplaceAll(trimmed, "\t", "    ")
		buf.WriteString(trimmed)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

type setupTest struct {
	summary string
	input   []string
	layers  []*setup.Layer
	result  *setup.Layer
	error   string
	order   map[string][]string
}

var setupTests = []setupTest{{
	summary: "Relatively simple layer with override on top",
	input: []string{`
		summary: Simple layer
		description: A simple layer.
		services:
			srv1:
				override: replace
				summary: Service summary
				command: cmd arg1 "arg2 arg3"
				default: start
				after:
					- srv2
				before:
					- srv3
				requires:
					- srv2
					- srv3
				environment:
					- var1: val1
					- var0: val0
					- var2: val2
			srv2:
				override: replace
				default: start
				command: cmd
				before:
					- srv3
			srv3:
				override: replace
				command: cmd
		`, `
		summary: Simple override layer.
		description: The second layer.
		services:
			srv1:
				override: merge
				environment:
					- var3: val3
				after:
					- srv4
				before:
					- srv5
			srv2:
				override: replace
				default: stop
				command: cmd
				summary: Replaced service
			srv4:
				override: replace
				command: cmd
				default: start
			srv5:
				override: replace
				command: cmd
	`},
	layers: []*setup.Layer{{
		Key:         "layer-0",
		Summary:     "Simple layer",
		Description: "A simple layer.",
		Services: map[string]*setup.Service{
			"srv1": {
				Name:     "srv1",
				Summary:  "Service summary",
				Override: "replace",
				Command:  `cmd arg1 "arg2 arg3"`,
				Default:  setup.StartAction,
				Before:   []string{"srv3"},
				After:    []string{"srv2"},
				Requires: []string{"srv2", "srv3"},
				Environment: []setup.StringVariable{
					{Name: "var1", Value: "val1"},
					{Name: "var0", Value: "val0"},
					{Name: "var2", Value: "val2"},
				},
			},
			"srv2": {
				Name:     "srv2",
				Override: "replace",
				Command:  "cmd",
				Default:  setup.StartAction,
				Before:   []string{"srv3"},
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  "cmd",
				Default:  setup.UnknownAction,
			},
		},
	}, {
		Key:         "layer-1",
		Summary:     "Simple override layer.",
		Description: "The second layer.",
		Services: map[string]*setup.Service{
			"srv1": {
				Name:     "srv1",
				Override: "merge",
				Before:   []string{"srv5"},
				After:    []string{"srv4"},
				Environment: []setup.StringVariable{
					{Name: "var3", Value: "val3"},
				},
			},
			"srv2": {
				Name:     "srv2",
				Summary:  "Replaced service",
				Override: "replace",
				Command:  "cmd",
				Default:  setup.StopAction,
			},
			"srv4": {
				Name:     "srv4",
				Override: "replace",
				Command:  "cmd",
				Default:  setup.StartAction,
			},
			"srv5": {
				Name:     "srv5",
				Override: "replace",
				Command:  "cmd",
			},
		},
	}},
	result: &setup.Layer{
		Key:         "",
		Summary:     "Simple override layer.",
		Description: "The second layer.",
		Services: map[string]*setup.Service{
			"srv1": {
				Name:     "srv1",
				Summary:  "Service summary",
				Override: "replace",
				Command:  `cmd arg1 "arg2 arg3"`,
				Default:  "start",
				After:    []string{"srv2", "srv4"},
				Before:   []string{"srv3", "srv5"},
				Requires: []string{"srv2", "srv3"},
				Environment: []setup.StringVariable{
					{Name: "var1", Value: "val1"},
					{Name: "var0", Value: "val0"},
					{Name: "var2", Value: "val2"},
					{Name: "var3", Value: "val3"},
				},
			},
			"srv2": {
				Name:     "srv2",
				Summary:  "Replaced service",
				Override: "replace",
				Command:  "cmd",
				Default:  "stop",
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  "cmd",
			},
			"srv4": {
				Name:     "srv4",
				Override: "replace",
				Command:  "cmd",
				Default:  "start",
			},
			"srv5": &setup.Service{
				Name:     "srv5",
				Override: "replace",
				Command:  "cmd",
			},
		},
	},
	order: map[string][]string{
		"srv1": []string{"srv2", "srv1", "srv3"},
		"srv2": []string{"srv2"},
		"srv3": []string{"srv3"},
	},
}, {
	summary: "Order loop on before/after",
	error:   "services in before/after loop: srv1, srv2, srv3",
	input: []string{`
		summary: Order loop
		services:
			srv1:
				override: replace
				command: cmd
				after:
					- srv3
			srv2:
				override: replace
				command: cmd
				after:
					- srv1
				before:
					- srv3
			srv3:
				override: replace
				command: cmd
	`},
}, {

	summary: "Handling of nulls and typed values in environment",
	input: []string{`
		services:
			srv1:
				override: replace
				command: cmd
				environment:
					- a: true
					- b: 1.1
					- c:
	`},
	layers: []*setup.Layer{{
		Key: "layer-0",
		Services: map[string]*setup.Service{
			"srv1": {
				Name:     "srv1",
				Override: "replace",
				Command:  "cmd",
				Environment: []setup.StringVariable{
					{Name: "a", Value: "true"},
					{Name: "b", Value: "1.1"},
					{Name: "c", Value: ""},
				},
			},
		},
	}},
}, {

	summary: "Unknown keys are not accepted",
	error:   "(?s).*field future not found.*",
	input: []string{`
		services:
			srv1:
				future: true
				override: replace
				command: cmd
	`},
}}

func (s *S) TestSetupTests(c *C) {

	for _, test := range setupTests {
		var sup setup.Setup
		var err error
		for i, yml := range test.input {
			layer, e := setup.ParseLayer(fmt.Sprintf("layer-%d", i), reindent(yml))
			if e != nil {
				err = e
				break
			}
			if len(test.layers) > 0 && test.layers[i] != nil {
				c.Assert(layer, DeepEquals, test.layers[i])
			}
			sup.AddLayer(layer)
		}
		if err == nil {
			var result *setup.Layer
			result, err = sup.Flatten()
			if err == nil && test.result != nil {
				c.Assert(result, DeepEquals, test.result)
			}
			if err == nil {
				for name, order := range test.order {
					names, err := result.StartOrder([]string{name})
					c.Assert(err, IsNil)
					c.Assert(names, DeepEquals, order)
				}
			}
		}
		if err != nil || test.error != "" {
			if test.error != "" {
				c.Assert(err, ErrorMatches, test.error)
			} else {
				c.Assert(err, IsNil)
			}
		}
	}
}
