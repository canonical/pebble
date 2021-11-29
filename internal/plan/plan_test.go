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

package plan_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internal/plan"
)

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second
)

// TODOs:
// - command-chain
// - error on invalid keys
// - constraints on service names

// The YAML on tests below passes through this function to deindent and
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

type planTest struct {
	summary string
	input   []string
	layers  []*plan.Layer
	result  *plan.Layer
	error   string
	start   map[string][]string
	stop    map[string][]string
}

var planTests = []planTest{{
	summary: "Relatively simple layer with override on top",
	input: []string{`
		summary: Simple layer
		description: A simple layer.
		services:
			srv1:
				override: replace
				summary: Service summary
				command: cmd arg1 "arg2 arg3"
				startup: enabled
				after:
					- srv2
				before:
					- srv3
				requires:
					- srv2
					- srv3
				environment:
					var1: val1
					var0: val0
					var2: val2
				backoff-delay: 1s
				backoff-factor: 1.5
				backoff-limit: 10s
			srv2:
				override: replace
				startup: enabled
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
					var3: val3
				after:
					- srv4
				before:
					- srv5
			srv2:
				override: replace
				startup: disabled
				command: cmd
				summary: Replaced service
			srv4:
				override: replace
				command: cmd
				startup: enabled
			srv5:
				override: replace
				command: cmd
	`},
	layers: []*plan.Layer{{
		Order:       0,
		Label:       "layer-0",
		Summary:     "Simple layer",
		Description: "A simple layer.",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Summary:  "Service summary",
				Override: "replace",
				Command:  `cmd arg1 "arg2 arg3"`,
				Startup:  plan.StartupEnabled,
				Before:   []string{"srv3"},
				After:    []string{"srv2"},
				Requires: []string{"srv2", "srv3"},
				Environment: map[string]string{
					"var1": "val1",
					"var0": "val0",
					"var2": "val2",
				},
				BackoffDelay:  plan.OptionalDuration{Value: time.Second, IsSet: true},
				BackoffFactor: plan.OptionalFloat{Value: 1.5, IsSet: true},
				BackoffLimit:  plan.OptionalDuration{Value: 10 * time.Second, IsSet: true},
			},
			"srv2": {
				Name:          "srv2",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupEnabled,
				Before:        []string{"srv3"},
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv3": {
				Name:          "srv3",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupUnknown,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
	}, {
		Order:       1,
		Label:       "layer-1",
		Summary:     "Simple override layer.",
		Description: "The second layer.",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Override: "merge",
				Before:   []string{"srv5"},
				After:    []string{"srv4"},
				Environment: map[string]string{
					"var3": "val3",
				},
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv2": {
				Name:          "srv2",
				Summary:       "Replaced service",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupDisabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv4": {
				Name:          "srv4",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv5": {
				Name:          "srv5",
				Override:      "replace",
				Command:       "cmd",
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
	}},
	result: &plan.Layer{
		Summary:     "Simple override layer.",
		Description: "The second layer.",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Summary:  "Service summary",
				Override: "replace",
				Command:  `cmd arg1 "arg2 arg3"`,
				Startup:  plan.StartupEnabled,
				After:    []string{"srv2", "srv4"},
				Before:   []string{"srv3", "srv5"},
				Requires: []string{"srv2", "srv3"},
				Environment: map[string]string{
					"var1": "val1",
					"var0": "val0",
					"var2": "val2",
					"var3": "val3",
				},
				BackoffDelay:  plan.OptionalDuration{Value: time.Second, IsSet: true},
				BackoffFactor: plan.OptionalFloat{Value: 1.5, IsSet: true},
				BackoffLimit:  plan.OptionalDuration{Value: 10 * time.Second, IsSet: true},
			},
			"srv2": {
				Name:          "srv2",
				Summary:       "Replaced service",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupDisabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv3": {
				Name:          "srv3",
				Override:      "replace",
				Command:       "cmd",
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv4": {
				Name:          "srv4",
				Override:      "replace",
				Command:       "cmd",
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"srv5": {
				Name:          "srv5",
				Override:      "replace",
				Command:       "cmd",
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
	},
	start: map[string][]string{
		"srv1": {"srv2", "srv1", "srv3"},
		"srv2": {"srv2"},
		"srv3": {"srv3"},
	},
	stop: map[string][]string{
		"srv1": {"srv1"},
		"srv2": {"srv1", "srv2"},
		"srv3": {"srv3", "srv1"},
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
					a: true
					b: 1.1
					c:
	`},
	layers: []*plan.Layer{{
		Order: 0,
		Label: "layer-0",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Override: "replace",
				Command:  "cmd",
				Environment: map[string]string{
					"a": "true",
					"b": "1.1",
					"c": "",
				},
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
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
}, {
	summary: `Cannot use service name "pebble"`,
	error:   `cannot use reserved service name "pebble"`,
	input: []string{`
		services:
			pebble:
				command: cmd
	`},
}, {
	summary: `Cannot have null service definition`,
	error:   `service object cannot be null for service "svc1"`,
	input: []string{`
		services:
			svc1: ~
	`},
}, {
	summary: `Cannot use empty string as service name`,
	error:   "cannot use empty string as service name",
	input: []string{`
		services:
			"":
				override: replace
				command: cmd
	`},
}, {
	summary: `Invalid action`,
	error:   `invalid on-success action "foo"`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				on-success: foo
	`},
}, {
	summary: `Invalid backoff-delay duration`,
	error:   `cannot parse layer "layer-0": invalid duration "foo"`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				backoff-delay: foo
	`},
}, {
	summary: `Zero backoff-factor`,
	error:   `backoff-factor must be 1.0 or greater, not 0`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				backoff-factor: 0
	`},
}, {
	summary: `Too small backoff-factor`,
	error:   `backoff-factor must be 1.0 or greater, not 0.5`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				backoff-factor: 0.5
	`},
}, {
	summary: `Invalid backoff-factor`,
	error:   `cannot parse layer "layer-0": invalid floating-point number "foo"`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				backoff-factor: foo
	`},
}}

func (s *S) TestParseLayer(c *C) {
	for _, test := range planTests {
		var sup plan.Plan
		var err error
		for i, yml := range test.input {
			layer, e := plan.ParseLayer(i, fmt.Sprintf("layer-%d", i), reindent(yml))
			if e != nil {
				err = e
				break
			}
			if len(test.layers) > 0 && test.layers[i] != nil {
				c.Assert(layer, DeepEquals, test.layers[i])
			}
			sup.Layers = append(sup.Layers, layer)
		}
		if err == nil {
			var result *plan.Layer
			result, err = plan.CombineLayers(sup.Layers...)
			if err == nil && test.result != nil {
				c.Assert(result, DeepEquals, test.result)
			}
			if err == nil {
				for name, order := range test.start {
					p := plan.Plan{Services: result.Services}
					names, err := p.StartOrder([]string{name})
					c.Assert(err, IsNil)
					c.Assert(names, DeepEquals, order)
				}
				for name, order := range test.stop {
					p := plan.Plan{Services: result.Services}
					names, err := p.StopOrder([]string{name})
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

func (s *S) TestCombineLayersCycle(c *C) {
	// Even if individual layers don't have cycles, combined layers might.
	layer1, err := plan.ParseLayer(1, "label1", []byte(`
services:
    srv1:
        override: replace
        command: cmd
        after:
            - srv2
`))
	c.Assert(err, IsNil)
	layer2, err := plan.ParseLayer(2, "label2", []byte(`
services:
    srv2:
        override: replace
        command: cmd
        after:
            - srv1
`))
	c.Assert(err, IsNil)
	_, err = plan.CombineLayers(layer1, layer2)
	c.Assert(err, ErrorMatches, `services in before/after loop: .*`)
	_, ok := err.(*plan.FormatError)
	c.Assert(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))
}

func (s *S) TestMissingOverride(c *C) {
	layer1, err := plan.ParseLayer(1, "label1", []byte("{}"))
	c.Assert(err, IsNil)
	layer2, err := plan.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        command: cmd
`))
	c.Assert(err, IsNil)
	_, err = plan.CombineLayers(layer1, layer2)
	c.Check(err, ErrorMatches, `layer "label2" must define \"override\" for service "srv1"`)
	_, ok := err.(*plan.FormatError)
	c.Check(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))
}

func (s *S) TestMissingCommand(c *C) {
	// Combine fails if no command in combined plan
	layer1, err := plan.ParseLayer(1, "label1", []byte("{}"))
	c.Assert(err, IsNil)
	layer2, err := plan.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        override: merge
`))
	c.Assert(err, IsNil)
	_, err = plan.CombineLayers(layer1, layer2)
	c.Check(err, ErrorMatches, `plan must define "command" for service "srv1"`)
	_, ok := err.(*plan.FormatError)
	c.Check(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))

	// Combine succeeds if there is a command in combined plan
	layer1, err = plan.ParseLayer(1, "label1", []byte(`
services:
    srv1:
        override: merge
        command: foo --bar
`))
	c.Assert(err, IsNil)
	layer2, err = plan.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        override: merge
`))
	c.Assert(err, IsNil)
	combined, err := plan.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	c.Assert(combined.Services["srv1"].Command, Equals, "foo --bar")
}

func (s *S) TestReadDir(c *C) {
	pebbleDir := c.MkDir()
	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, test := range planTests {
		for i, yml := range test.input {
			err := ioutil.WriteFile(filepath.Join(layersDir, fmt.Sprintf("%03d-layer-%d.yaml", i, i)), []byte(reindent(yml)), 0644)
			c.Assert(err, IsNil)
		}
		sup, err := plan.ReadDir(pebbleDir)
		if err == nil {
			var result *plan.Layer
			result, err = plan.CombineLayers(sup.Layers...)
			if err == nil && test.result != nil {
				c.Assert(result, DeepEquals, test.result)
			}
			if err == nil {
				for name, order := range test.start {
					p := plan.Plan{Services: result.Services}
					names, err := p.StartOrder([]string{name})
					c.Assert(err, IsNil)
					c.Assert(names, DeepEquals, order)
				}
				for name, order := range test.stop {
					p := plan.Plan{Services: result.Services}
					names, err := p.StopOrder([]string{name})
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

var readDirBadNames = []string{
	"001-l.yaml",
	"01-label.yaml",
	"0001-label.yaml",
	"0001-label.yaml",
	"001-label-.yaml",
	"001--label.yaml",
	"001-label--label.yaml",
}

func (s *S) TestReadDirBadNames(c *C) {
	pebbleDir := c.MkDir()
	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, fname := range readDirBadNames {
		fpath := filepath.Join(layersDir, fname)
		err := ioutil.WriteFile(fpath, []byte("<ignore>"), 0644)
		c.Assert(err, IsNil)
		_, err = plan.ReadDir(pebbleDir)
		c.Assert(err.Error(), Equals, fmt.Sprintf("invalid layer filename: %q (must look like \"123-some-label.yaml\")", fname))
		err = os.Remove(fpath)
		c.Assert(err, IsNil)
	}
}

var readDirDupNames = [][]string{
	{"001-bar.yaml", "001-foo.yaml"},
	{"001-foo.yaml", "002-foo.yaml"},
}

func (s *S) TestReadDirDupNames(c *C) {
	pebbleDir := c.MkDir()
	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)

	for _, fnames := range readDirDupNames {
		for _, fname := range fnames {
			fpath := filepath.Join(layersDir, fname)
			err := ioutil.WriteFile(fpath, []byte("summary: ignore"), 0644)
			c.Assert(err, IsNil)
		}
		_, err = plan.ReadDir(pebbleDir)
		c.Assert(err.Error(), Equals, fmt.Sprintf("invalid layer filename: %q not unique (have %q already)", fnames[1], fnames[0]))
		for _, fname := range fnames {
			fpath := filepath.Join(layersDir, fname)
			err = os.Remove(fpath)
			c.Assert(err, IsNil)
		}
	}
}

func (s *S) TestMarshalLayer(c *C) {
	layerBytes := reindent(`
		summary: Simple layer
		description: A simple layer.
		services:
			srv1:
				summary: Service summary
				startup: enabled
				override: replace
				command: cmd arg1 "arg2 arg3"
				after:
					- srv2
				before:
					- srv3
				requires:
					- srv2
					- srv3
				environment:
					var0: val0
					var1: val1
					var2: val2
				backoff-delay: 1s
				backoff-factor: 1.5
				backoff-limit: 10s
			srv2:
				override: replace
				command: srv2cmd
			srv3:
				override: replace
				command: srv3cmd`)
	layer, err := plan.ParseLayer(1, "layer1", layerBytes)
	c.Assert(err, IsNil)
	out, err := yaml.Marshal(layer)
	c.Assert(err, IsNil)
	c.Assert(string(out), Equals, string(layerBytes))
}
