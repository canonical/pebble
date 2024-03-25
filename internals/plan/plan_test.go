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
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
)

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second

	defaultCheckPeriod    = 10 * time.Second
	defaultCheckTimeout   = 3 * time.Second
	defaultCheckThreshold = 3
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
				kill-delay: 10s
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
				working-dir: /workdir/srv1
			srv2:
				override: replace
				startup: enabled
				command: cmd
				before:
					- srv3
				working-dir: /workdir/srv2
			srv3:
				override: replace
				command: cmd
			srv6:
				override: replace
				command: cmd6a
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
				working-dir: /workdir/srv1/override
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
			srv6:
				override: merge
				command: cmd6b
				environment:
					foo: bar
					baz: buz
				on-check-failure:
					chk1: restart
	`},
	layers: []*plan.Layer{{
		Order:       0,
		Label:       "layer-0",
		Summary:     "Simple layer",
		Description: "A simple layer.",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:      "srv1",
				Summary:   "Service summary",
				Override:  "replace",
				Command:   `cmd arg1 "arg2 arg3"`,
				KillDelay: plan.OptionalDuration{Value: time.Second * 10, IsSet: true},
				Startup:   plan.StartupEnabled,
				Before:    []string{"srv3"},
				After:     []string{"srv2"},
				Requires:  []string{"srv2", "srv3"},
				Environment: map[string]string{
					"var1": "val1",
					"var0": "val0",
					"var2": "val2",
				},
				WorkingDir:    "/workdir/srv1",
				BackoffDelay:  plan.OptionalDuration{Value: time.Second, IsSet: true},
				BackoffFactor: plan.OptionalFloat{Value: 1.5, IsSet: true},
				BackoffLimit:  plan.OptionalDuration{Value: 10 * time.Second, IsSet: true},
			},
			"srv2": {
				Name:       "srv2",
				Override:   "replace",
				Command:    "cmd",
				WorkingDir: "/workdir/srv2",
				Startup:    plan.StartupEnabled,
				Before:     []string{"srv3"},
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  "cmd",
				Startup:  plan.StartupUnknown,
			},
			"srv6": {
				Name:     "srv6",
				Override: "replace",
				Command:  "cmd6a",
				Startup:  plan.StartupUnknown,
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
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
				WorkingDir: "/workdir/srv1/override",
			},
			"srv2": {
				Name:     "srv2",
				Summary:  "Replaced service",
				Override: "replace",
				Command:  "cmd",
				Startup:  plan.StartupDisabled,
			},
			"srv4": {
				Name:     "srv4",
				Override: "replace",
				Command:  "cmd",
				Startup:  plan.StartupEnabled,
			},
			"srv5": {
				Name:     "srv5",
				Override: "replace",
				Command:  "cmd",
			},
			"srv6": {
				Name:     "srv6",
				Override: "merge",
				Command:  "cmd6b",
				Startup:  plan.StartupUnknown,
				Environment: map[string]string{
					"foo": "bar",
					"baz": "buz",
				},
				OnCheckFailure: map[string]plan.ServiceAction{
					"chk1": plan.ActionRestart,
				},
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	}},
	result: &plan.Layer{
		Summary:     "Simple override layer.",
		Description: "The second layer.",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:      "srv1",
				Summary:   "Service summary",
				Override:  "replace",
				Command:   `cmd arg1 "arg2 arg3"`,
				KillDelay: plan.OptionalDuration{Value: time.Second * 10, IsSet: true},
				Startup:   plan.StartupEnabled,
				After:     []string{"srv2", "srv4"},
				Before:    []string{"srv3", "srv5"},
				Requires:  []string{"srv2", "srv3"},
				Environment: map[string]string{
					"var1": "val1",
					"var0": "val0",
					"var2": "val2",
					"var3": "val3",
				},
				WorkingDir:    "/workdir/srv1/override",
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
			"srv6": {
				Name:     "srv6",
				Override: "replace",
				Command:  "cmd6b",
				Environment: map[string]string{
					"foo": "bar",
					"baz": "buz",
				},
				OnCheckFailure: map[string]plan.ServiceAction{
					"chk1": plan.ActionRestart,
				},
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
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
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
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
	error:   `plan service "svc1" on-success action "foo" invalid`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				on-success: foo
	`},
}, {
	summary: `Invalid on-success success-shutdown`,
	error:   `plan service "svc1" on-success action "success-shutdown" invalid`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				on-success: success-shutdown
	`},
}, {
	summary: `Invalid on-failure failure-shutdown`,
	error:   `plan service "svc1" on-failure action "failure-shutdown" invalid`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				on-failure: failure-shutdown
	`},
}, {
	summary: `Invalid on-check-failure failure-shutdown`,
	error:   `plan service "svc1" on-check-failure action "failure-shutdown" invalid`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				on-check-failure:
					test: failure-shutdown
		checks:
			test:
				override: replace
				http:
					url: https://example.com/foo
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
	error:   `plan service "svc1" backoff-factor must be 1.0 or greater, not 0`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd
				backoff-factor: 0
	`},
}, {
	summary: `Too small backoff-factor`,
	error:   `plan service "svc1" backoff-factor must be 1.0 or greater, not 0.5`,
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
}, {
	summary: `Invalid service command`,
	error:   `plan service "svc1" command invalid: cannot parse service "svc1" command: EOF found when expecting closing quote`,
	input: []string{`
		services:
			svc1:
				override: replace
				command: foo '
	`},
}, {
	summary: `Optional/overridable arguments in service command`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd -v [ --foo bar -e "x [ y ] z" ]
	`},
	layers: []*plan.Layer{{
		Order: 0,
		Label: "layer-0",
		Services: map[string]*plan.Service{
			"svc1": {
				Name:     "svc1",
				Override: "replace",
				Command:  `cmd -v [ --foo bar -e "x [ y ] z" ]`,
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	}},
}, {
	summary: `Invalid service command: cannot have any arguments after [ ... ] group`,
	error:   `plan service "svc1" command invalid: cannot parse service "svc1" command: cannot have any arguments after \[ ... \] group`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd -v [ --foo ] bar
	`},
}, {
	summary: `Invalid service command: cannot have ] outside of [ ... ] group`,
	error:   `plan service "svc1" command invalid: cannot parse service "svc1" command: cannot have \] outside of \[ ... \] group`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd -v ] foo
	`},
}, {
	summary: `Invalid service command: cannot nest [ ... ] groups`,
	error:   `plan service "svc1" command invalid: cannot parse service "svc1" command: cannot nest \[ ... \] groups`,
	input: []string{`
		services:
			"svc1":
				override: replace
				command: cmd -v [ foo [ --bar ] ]
	`},
}, {
	summary: "Checks fields parse correctly and defaults are correct",
	input: []string{`
		checks:
			chk-http:
				override: replace
				level: alive
				period: 20s
				timeout: 500ms
				threshold: 7
				http:
					url: https://example.com/foo
					headers:
						Foo: bar
						Authorization: Basic password
		
			chk-tcp:
				override: merge
				level: ready
				tcp:
					port: 7777
					host: somehost
		
			chk-exec:
				override: replace
				exec:
					command: sleep 1
					environment:
						FOO: bar
						BAZ: buzz
					working-dir: /root
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks: map[string]*plan.Check{
			"chk-http": {
				Name:      "chk-http",
				Override:  plan.ReplaceOverride,
				Level:     plan.AliveLevel,
				Period:    plan.OptionalDuration{Value: 20 * time.Second, IsSet: true},
				Timeout:   plan.OptionalDuration{Value: 500 * time.Millisecond, IsSet: true},
				Threshold: 7,
				HTTP: &plan.HTTPCheck{
					URL: "https://example.com/foo",
					Headers: map[string]string{
						"Foo":           "bar",
						"Authorization": "Basic password",
					},
				},
			},
			"chk-tcp": {
				Name:      "chk-tcp",
				Override:  plan.MergeOverride,
				Level:     plan.ReadyLevel,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: defaultCheckTimeout},
				Threshold: defaultCheckThreshold,
				TCP: &plan.TCPCheck{
					Port: 7777,
					Host: "somehost",
				},
			},
			"chk-exec": {
				Name:      "chk-exec",
				Override:  plan.ReplaceOverride,
				Level:     plan.UnsetLevel,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: defaultCheckTimeout},
				Threshold: defaultCheckThreshold,
				Exec: &plan.ExecCheck{
					Command: "sleep 1",
					Environment: map[string]string{
						"FOO": "bar",
						"BAZ": "buzz",
					},
					WorkingDir: "/root",
				},
			},
		},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "Checks override replace works correctly",
	input: []string{`
		checks:
			chk-http:
				override: replace
				period: 20s
				http:
					url: https://example.com/foo
					headers:
						Foo: bar
		
			chk-tcp:
				override: replace
				level: ready
				tcp:
					port: 7777
					host: somehost
		
			chk-exec:
				override: replace
				exec:
					command: sleep 1
					working-dir: /root
`, `
		checks:
			chk-http:
				override: replace
				http:
					url: https://example.com/bar
		
			chk-tcp:
				override: replace
				tcp:
					port: 8888
		
			chk-exec:
				override: replace
				exec:
					command: sleep 2
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks: map[string]*plan.Check{
			"chk-http": {
				Name:      "chk-http",
				Override:  plan.ReplaceOverride,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: defaultCheckTimeout},
				Threshold: defaultCheckThreshold,
				HTTP: &plan.HTTPCheck{
					URL: "https://example.com/bar",
				},
			},
			"chk-tcp": {
				Name:      "chk-tcp",
				Override:  plan.ReplaceOverride,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: defaultCheckTimeout},
				Threshold: defaultCheckThreshold,
				TCP: &plan.TCPCheck{
					Port: 8888,
				},
			},
			"chk-exec": {
				Name:      "chk-exec",
				Override:  plan.ReplaceOverride,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: defaultCheckTimeout},
				Threshold: defaultCheckThreshold,
				Exec: &plan.ExecCheck{
					Command: "sleep 2",
				},
			},
		},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "Checks override merge works correctly",
	input: []string{`
		checks:
			chk-http:
				override: merge
				period: 1s
		
			chk-tcp:
				override: merge
				timeout: 300ms
				tcp:
					host: foobar
		
			chk-exec:
				override: merge
				threshold: 5
				exec:
					working-dir: /root
`, `
		checks:
			chk-http:
				override: merge
				http:
					url: https://example.com/bar
					headers:
						Foo: bar
		
			chk-tcp:
				override: merge
				tcp:
					port: 80
		
			chk-exec:
				override: merge
				timeout: 7s
				exec:
					command: sleep 2
					environment:
						FOO: bar
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks: map[string]*plan.Check{
			"chk-http": {
				Name:      "chk-http",
				Override:  plan.MergeOverride,
				Period:    plan.OptionalDuration{Value: time.Second, IsSet: true},
				Timeout:   plan.OptionalDuration{Value: time.Second},
				Threshold: defaultCheckThreshold,
				HTTP: &plan.HTTPCheck{
					URL:     "https://example.com/bar",
					Headers: map[string]string{"Foo": "bar"},
				},
			},
			"chk-tcp": {
				Name:      "chk-tcp",
				Override:  plan.MergeOverride,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: 300 * time.Millisecond, IsSet: true},
				Threshold: defaultCheckThreshold,
				TCP: &plan.TCPCheck{
					Port: 80,
					Host: "foobar",
				},
			},
			"chk-exec": {
				Name:      "chk-exec",
				Override:  plan.MergeOverride,
				Period:    plan.OptionalDuration{Value: defaultCheckPeriod},
				Timeout:   plan.OptionalDuration{Value: 7 * time.Second, IsSet: true},
				Threshold: 5,
				Exec: &plan.ExecCheck{
					Command:    "sleep 2",
					WorkingDir: "/root",
					Environment: map[string]string{
						"FOO": "bar",
					},
				},
			},
		},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "Timeout is capped at period",
	input: []string{`
		checks:
			chk1:
				override: replace
				period: 100ms
				timeout: 2s
				tcp:
					host: foobar
					port: 80
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  plan.ReplaceOverride,
				Period:    plan.OptionalDuration{Value: 100 * time.Millisecond, IsSet: true},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond, IsSet: true},
				Threshold: defaultCheckThreshold,
				TCP: &plan.TCPCheck{
					Port: 80,
					Host: "foobar",
				},
			},
		},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "Unset timeout is capped at period",
	input: []string{`
		checks:
			chk1:
				override: replace
				period: 100ms
				tcp:
					host: foobar
					port: 80
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks: map[string]*plan.Check{
			"chk1": {
				Name:      "chk1",
				Override:  plan.ReplaceOverride,
				Period:    plan.OptionalDuration{Value: 100 * time.Millisecond, IsSet: true},
				Timeout:   plan.OptionalDuration{Value: 100 * time.Millisecond, IsSet: false},
				Threshold: defaultCheckThreshold,
				TCP: &plan.TCPCheck{
					Port: 80,
					Host: "foobar",
				},
			},
		},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "One of http, tcp, or exec must be present for check",
	error:   `plan must specify one of "http", "tcp", or "exec" for check "chk1"`,
	input: []string{`
		checks:
			chk1:
				override: replace
`},
}, {
	summary: "HTTP check requires url field",
	error:   `plan must set "url" for http check "chk1"`,
	input: []string{`
		checks:
			chk1:
				override: replace
				http: {}
`},
}, {
	summary: "TCP check requires port field",
	error:   `plan must set "port" for tcp check "chk2"`,
	input: []string{`
		checks:
			chk2:
				override: replace
				tcp: {}
`},
}, {
	summary: "Exec check requires command field",
	error:   `plan must set "command" for exec check "chk3"`,
	input: []string{`
		checks:
			chk3:
				override: replace
				exec: {}
`},
}, {
	summary: `Invalid exec check command`,
	error:   `plan check "chk1" command invalid: EOF found when expecting closing quote`,
	input: []string{`
		checks:
			chk1:
				override: replace
				exec:
					command: foo '
	`},
}, {
	summary: `Invalid exec check service context`,
	error:   `plan check "chk1" service context specifies non-existent service "nosvc"`,
	input: []string{`
		checks:
			chk1:
				override: replace
				exec:
					command: foo
					service-context: nosvc
	`},
}, {
	summary: "Simple layer with log targets",
	input: []string{`
		services:
			svc1:
				command: foo
				override: merge
				startup: enabled
			svc2:
				command: bar
				override: merge
				startup: enabled
		
		log-targets:
			tgt1:
				type: loki
				location: http://10.1.77.196:3100/loki/api/v1/push
				services: [all]
				override: merge
			tgt2:
				type: syslog
				location: udp://0.0.0.0:514
				services: [svc2]
				override: merge
`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{
			"svc1": {
				Name:          "svc1",
				Command:       "foo",
				Override:      plan.MergeOverride,
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"svc2": {
				Name:          "svc2",
				Command:       "bar",
				Override:      plan.MergeOverride,
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
		Checks: map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Type:     plan.LokiTarget,
				Location: "http://10.1.77.196:3100/loki/api/v1/push",
				Services: []string{"all"},
				Override: plan.MergeOverride,
			},
			"tgt2": {
				Name:     "tgt2",
				Type:     plan.SyslogTarget,
				Location: "udp://0.0.0.0:514",
				Services: []string{"svc2"},
				Override: plan.MergeOverride,
			},
		},
		Sections: map[string]plan.LayerSection{},
	},
}, {
	summary: "Overriding log targets",
	input: []string{`
		services:
			svc1:
				command: foo
				override: merge
				startup: enabled
			svc2:
				command: bar
				override: merge
				startup: enabled
		
		log-targets:
			tgt1:
				type: loki
				location: http://10.1.77.196:3100/loki/api/v1/push
				services: [all]
				override: merge
			tgt2:
				type: syslog
				location: udp://0.0.0.0:514
				services: [svc2]
				override: merge
			tgt3:
				type: loki
				location: http://10.1.77.206:3100/loki/api/v1/push
				services: [all]
				override: merge
`, `
		services:
			svc1:
				command: foo
				override: merge
			svc2:
				command: bar
				override: replace
				startup: enabled
		
		log-targets:
			tgt1:
				services: [-all, svc1]
				override: merge
			tgt2:
				type: syslog
				location: udp://1.2.3.4:514
				services: []
				override: replace
			tgt3:
				type: syslog
				location: udp://0.0.0.0:514
				services: [-svc1]
				override: merge
`},
	layers: []*plan.Layer{{
		Label: "layer-0",
		Order: 0,
		Services: map[string]*plan.Service{
			"svc1": {
				Name:     "svc1",
				Command:  "foo",
				Override: plan.MergeOverride,
				Startup:  plan.StartupEnabled,
			},
			"svc2": {
				Name:     "svc2",
				Command:  "bar",
				Override: plan.MergeOverride,
				Startup:  plan.StartupEnabled,
			},
		},
		Checks: map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Type:     plan.LokiTarget,
				Location: "http://10.1.77.196:3100/loki/api/v1/push",
				Services: []string{"all"},
				Override: plan.MergeOverride,
			},
			"tgt2": {
				Name:     "tgt2",
				Type:     plan.SyslogTarget,
				Location: "udp://0.0.0.0:514",
				Services: []string{"svc2"},
				Override: plan.MergeOverride,
			},
			"tgt3": {
				Name:     "tgt3",
				Type:     plan.LokiTarget,
				Location: "http://10.1.77.206:3100/loki/api/v1/push",
				Services: []string{"all"},
				Override: plan.MergeOverride,
			},
		},
		Sections: map[string]plan.LayerSection{},
	}, {
		Label: "layer-1",
		Order: 1,
		Services: map[string]*plan.Service{
			"svc1": {
				Name:     "svc1",
				Command:  "foo",
				Override: plan.MergeOverride,
			},
			"svc2": {
				Name:     "svc2",
				Command:  "bar",
				Override: plan.ReplaceOverride,
				Startup:  plan.StartupEnabled,
			},
		},
		Checks: map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Services: []string{"-all", "svc1"},
				Override: plan.MergeOverride,
			},
			"tgt2": {
				Name:     "tgt2",
				Type:     plan.SyslogTarget,
				Location: "udp://1.2.3.4:514",
				Services: []string{},
				Override: plan.ReplaceOverride,
			},
			"tgt3": {
				Name:     "tgt3",
				Type:     plan.SyslogTarget,
				Location: "udp://0.0.0.0:514",
				Services: []string{"-svc1"},
				Override: plan.MergeOverride,
			},
		},
		Sections: map[string]plan.LayerSection{},
	}},
	result: &plan.Layer{
		Services: map[string]*plan.Service{
			"svc1": {
				Name:          "svc1",
				Command:       "foo",
				Override:      plan.MergeOverride,
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
			"svc2": {
				Name:          "svc2",
				Command:       "bar",
				Override:      plan.ReplaceOverride,
				Startup:       plan.StartupEnabled,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
			},
		},
		Checks: map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Type:     plan.LokiTarget,
				Location: "http://10.1.77.196:3100/loki/api/v1/push",
				Services: []string{"all", "-all", "svc1"},
				Override: plan.MergeOverride,
			},
			"tgt2": {
				Name:     "tgt2",
				Type:     plan.SyslogTarget,
				Location: "udp://1.2.3.4:514",
				Override: plan.ReplaceOverride,
			},
			"tgt3": {
				Name:     "tgt3",
				Type:     plan.SyslogTarget,
				Location: "udp://0.0.0.0:514",
				Services: []string{"all", "-svc1"},
				Override: plan.MergeOverride,
			},
		},
		Sections: map[string]plan.LayerSection{},
	},
}, {
	summary: "Log target requires type field",
	error:   `plan must define "type" \("loki" or "syslog"\) for log target "tgt1"`,
	input: []string{`
		log-targets:
			tgt1:
				location: http://10.1.77.196:3100/loki/api/v1/push
				override: merge
`},
}, {
	summary: "Log target location must be specified",
	error:   `plan must define "location" for log target "tgt1"`,
	input: []string{`
		log-targets:
			tgt1:
				type: loki
				services: [all]
				override: merge
`}}, {
	summary: "Unsupported log target type",
	error:   `log target "tgt1" has unsupported type "foobar", must be "loki" or "syslog"`,
	input: []string{`
		log-targets:
			tgt1:
				type: foobar
				location: http://10.1.77.196:3100/loki/api/v1/push
				override: merge
`},
}, {
	summary: "Log target specifies invalid service",
	error:   `log target "tgt1" specifies unknown service "nonexistent"`,
	input: []string{`
		log-targets:
			tgt1:
				type: loki
				location: http://10.1.77.196:3100/loki/api/v1/push
				services: [nonexistent]
				override: merge
`},
}, {
	summary: `Service name can't start with "-"`,
	error:   `cannot use service name "-svc1": starting with "-" not allowed`,
	input: []string{`
		services:
			-svc1:
				command: foo
				override: merge
`},
}, {
	summary: "Log forwarding labels override",
	input: []string{`
		log-targets:
			tgt1:
				override: merge
				type: loki
				location: https://my.loki.server/loki/api/v1/push
				labels:
					label1: foo11
					label2: foo12
			tgt2:
				override: merge
				type: loki
				location: https://my.loki.server/loki/api/v1/push
				labels:
					label1: foo21
					label2: foo22
`, `
		log-targets:
			tgt1:
				override: merge
				labels:
					label2: bar12
					label3: bar13
			tgt2:
				override: replace
				type: loki
				location: https://new.loki.server/loki/api/v1/push
				labels:
					label2: bar22
					label3: bar23
`},
	layers: []*plan.Layer{{
		Order:    0,
		Label:    "layer-0",
		Services: map[string]*plan.Service{},
		Checks:   map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Override: plan.MergeOverride,
				Type:     plan.LokiTarget,
				Location: "https://my.loki.server/loki/api/v1/push",
				Labels: map[string]string{
					"label1": "foo11",
					"label2": "foo12",
				},
			},
			"tgt2": {
				Name:     "tgt2",
				Override: plan.MergeOverride,
				Type:     plan.LokiTarget,
				Location: "https://my.loki.server/loki/api/v1/push",
				Labels: map[string]string{
					"label1": "foo21",
					"label2": "foo22",
				},
			},
		},
		Sections: map[string]plan.LayerSection{},
	}, {
		Order:    1,
		Label:    "layer-1",
		Services: map[string]*plan.Service{},
		Checks:   map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Override: plan.MergeOverride,
				Labels: map[string]string{
					"label2": "bar12",
					"label3": "bar13",
				},
			},
			"tgt2": {
				Name:     "tgt2",
				Override: plan.ReplaceOverride,
				Type:     plan.LokiTarget,
				Location: "https://new.loki.server/loki/api/v1/push",
				Labels: map[string]string{
					"label2": "bar22",
					"label3": "bar23",
				},
			},
		},
		Sections: map[string]plan.LayerSection{},
	}},
	result: &plan.Layer{
		Services: map[string]*plan.Service{},
		Checks:   map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Override: plan.MergeOverride,
				Type:     plan.LokiTarget,
				Location: "https://my.loki.server/loki/api/v1/push",
				Labels: map[string]string{
					"label1": "foo11",
					"label2": "bar12",
					"label3": "bar13",
				},
			},
			"tgt2": {
				Name:     "tgt2",
				Override: plan.ReplaceOverride,
				Type:     plan.LokiTarget,
				Location: "https://new.loki.server/loki/api/v1/push",
				Labels: map[string]string{
					"label2": "bar22",
					"label3": "bar23",
				},
			},
		},
		Sections: map[string]plan.LayerSection{},
	},
}, {
	summary: "Reserved log target labels",
	input: []string{`
		log-targets:
			tgt1:
				override: merge
				type: loki
				location: https://my.loki.server/loki/api/v1/push
				labels:
					pebble_service: illegal
`},
	error: `log target "tgt1": label "pebble_service" uses reserved prefix "pebble_"`,
}, {
	summary: "Required field two layers deep",
	input: []string{`
			services:
				srv1:
					override: replace
					command: sleep 1000
	`, `
			services:
				srv1:
					override: merge
					environment:
						VAR1: foo
	`, `
			services:
				srv1:
					override: merge
					environment:
						VAR2: bar
	`},
	result: &plan.Layer{
		Services: map[string]*plan.Service{
			"srv1": {
				Name:          "srv1",
				Command:       "sleep 1000",
				Override:      plan.ReplaceOverride,
				BackoffDelay:  plan.OptionalDuration{Value: defaultBackoffDelay},
				BackoffFactor: plan.OptionalFloat{Value: defaultBackoffFactor},
				BackoffLimit:  plan.OptionalDuration{Value: defaultBackoffLimit},
				Environment: map[string]string{
					"VAR1": "foo",
					"VAR2": "bar",
				},
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
		Sections:   map[string]plan.LayerSection{},
	},
}, {
	summary: "Three layers missing command",
	input: []string{`
		services:
			srv1:
				override: replace
`, `
		services:
			srv1:
				override: merge
				environment:
					VAR1: foo
`, `
		services:
			srv1:
				override: merge
				environment:
					VAR2: bar
`},
	error: `plan must define "command" for service "srv1"`,
}}

func (s *S) TestParseLayer(c *C) {
	for _, test := range planTests {
		c.Logf(test.summary)
		p := plan.NewPlan()
		var err error
		for i, yml := range test.input {
			layer, e := p.ParseLayer(i, fmt.Sprintf("layer-%d", i), reindent(yml))
			if e != nil {
				err = e
				break
			}
			if len(test.layers) > 0 && test.layers[i] != nil {
				c.Assert(layer, DeepEquals, test.layers[i])
			}
			p.Layers = append(p.Layers, layer)
		}
		if err == nil {
			var combinedLayer *plan.Layer
			combinedLayer, err = p.CombineLayers(p.Layers...)
			if err == nil && test.result != nil {
				c.Assert(combinedLayer, DeepEquals, test.result)
			}
			if err == nil {
				combinedPlan := plan.NewCombinedPlan(combinedLayer)
				err = combinedPlan.Validate(p)
				if err == nil {
					p.Combined = combinedPlan
				}
			}
			if err == nil {
				for name, order := range test.start {
					names, err := p.Combined.StartOrder([]string{name})
					c.Assert(err, IsNil)
					c.Assert(names, DeepEquals, order)
				}
				for name, order := range test.stop {
					names, err := p.Combined.StopOrder([]string{name})
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
	p := plan.NewPlan()
	layer1, err := p.ParseLayer(1, "label1", []byte(`
services:
    srv1:
        override: replace
        command: cmd
        after:
            - srv2
`))
	c.Assert(err, IsNil)
	layer2, err := p.ParseLayer(2, "label2", []byte(`
services:
    srv2:
        override: replace
        command: cmd
        after:
            - srv1
`))
	c.Assert(err, IsNil)
	combinedLayer, err := p.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	combinedPlan := plan.NewCombinedPlan(combinedLayer)
	err = combinedPlan.Validate(p)
	c.Assert(err, ErrorMatches, `services in before/after loop: .*`)
	_, ok := err.(*plan.FormatError)
	c.Assert(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))
}

func (s *S) TestMissingOverride(c *C) {
	p := plan.NewPlan()
	layer1, err := p.ParseLayer(1, "label1", []byte("{}"))
	c.Assert(err, IsNil)
	layer2, err := p.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        command: cmd
`))
	c.Assert(err, IsNil)
	_, err = p.CombineLayers(layer1, layer2)
	c.Check(err, ErrorMatches, `layer "label2" must define \"override\" for service "srv1"`)
	_, ok := err.(*plan.FormatError)
	c.Check(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))
}

func (s *S) TestMissingCommand(c *C) {
	p := plan.NewPlan()
	// Combine fails if no command in combined plan
	layer1, err := p.ParseLayer(1, "label1", []byte("{}"))
	c.Assert(err, IsNil)
	layer2, err := p.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        override: merge
`))
	c.Assert(err, IsNil)
	combinedLayer, err := p.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	combinedPlan := plan.NewCombinedPlan(combinedLayer)
	err = combinedPlan.Validate(p)
	c.Check(err, ErrorMatches, `plan must define "command" for service "srv1"`)
	_, ok := err.(*plan.FormatError)
	c.Check(ok, Equals, true, Commentf("error must be *plan.FormatError, not %T", err))

	// Combine succeeds if there is a command in combined plan
	layer1, err = p.ParseLayer(1, "label1", []byte(`
services:
    srv1:
        override: merge
        command: foo --bar
`))
	c.Assert(err, IsNil)
	layer2, err = p.ParseLayer(2, "label2", []byte(`
services:
    srv1:
        override: merge
`))
	c.Assert(err, IsNil)
	combinedLayer, err = p.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	c.Assert(combinedLayer.Services["srv1"].Command, Equals, "foo --bar")
}

func (s *S) TestReadDir(c *C) {
	tempDir := c.MkDir()

	for testIndex, test := range planTests {
		c.Logf(test.summary)
		pebbleDir := filepath.Join(tempDir, fmt.Sprintf("pebble-%03d", testIndex))
		layersDir := filepath.Join(pebbleDir, "layers")
		err := os.MkdirAll(layersDir, 0755)
		c.Assert(err, IsNil)

		for i, yml := range test.input {
			err := os.WriteFile(filepath.Join(layersDir, fmt.Sprintf("%03d-layer-%d.yaml", i, i)), reindent(yml), 0644)
			c.Assert(err, IsNil)
		}
		p := plan.NewPlan()
		err = p.ReadDir(pebbleDir)
		if err == nil {
			for name, order := range test.start {
				names, err := p.Combined.StartOrder([]string{name})
				c.Assert(err, IsNil)
				c.Assert(names, DeepEquals, order)
			}
			for name, order := range test.stop {
				names, err := p.Combined.StopOrder([]string{name})
				c.Assert(err, IsNil)
				c.Assert(names, DeepEquals, order)
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
		err := os.WriteFile(fpath, []byte("<ignore>"), 0644)
		c.Assert(err, IsNil)
		p := plan.NewPlan()
		err = p.ReadDir(pebbleDir)
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
			err := os.WriteFile(fpath, []byte("summary: ignore"), 0644)
			c.Assert(err, IsNil)
		}
		p := plan.NewPlan()
		err = p.ReadDir(pebbleDir)
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
	p := plan.NewPlan()
	layer, err := p.ParseLayer(1, "layer1", layerBytes)
	c.Assert(err, IsNil)
	out, err := yaml.Marshal(layer)
	c.Assert(err, IsNil)
	c.Assert(string(out), Equals, string(layerBytes))
}

var cmdTests = []struct {
	summary            string
	command            string
	cmdArgs            []string
	expectedBase       []string
	expectedExtra      []string
	expectedNewCommand string
	error              string
}{{
	summary:            "No default arguments, no additional cmdArgs",
	command:            "cmd --foo bar",
	expectedBase:       []string{"cmd", "--foo", "bar"},
	expectedNewCommand: "cmd --foo bar",
}, {
	summary:            "No default arguments, add cmdArgs only",
	command:            "cmd --foo bar",
	cmdArgs:            []string{"-v", "--opt"},
	expectedBase:       []string{"cmd", "--foo", "bar"},
	expectedNewCommand: "cmd --foo bar [ -v --opt ]",
}, {
	summary:            "Override default arguments with empty cmdArgs",
	command:            "cmd [ --foo bar ]",
	expectedBase:       []string{"cmd"},
	expectedExtra:      []string{"--foo", "bar"},
	expectedNewCommand: "cmd",
}, {
	summary:            "Override default arguments with cmdArgs",
	command:            "cmd [ --foo bar ]",
	cmdArgs:            []string{"--bar", "foo"},
	expectedBase:       []string{"cmd"},
	expectedExtra:      []string{"--foo", "bar"},
	expectedNewCommand: "cmd [ --bar foo ]",
}, {
	summary:            "Empty [ ... ], no cmdArgs",
	command:            "cmd --foo bar [ ]",
	expectedBase:       []string{"cmd", "--foo", "bar"},
	expectedNewCommand: "cmd --foo bar",
}, {
	summary:            "Empty [ ... ], override with cmdArgs",
	command:            "cmd --foo bar [ ]",
	cmdArgs:            []string{"-v", "--opt"},
	expectedBase:       []string{"cmd", "--foo", "bar"},
	expectedNewCommand: "cmd --foo bar [ -v --opt ]",
}, {
	summary: "[ ... ] should be a suffix",
	command: "cmd [ --foo ] --bar",
	error:   `cannot parse service "svc" command: cannot have any arguments after \[ ... \] group`,
}, {
	summary: "[ ... ] should not be prefix",
	command: "[ cmd --foo ]",
	error:   `cannot parse service "svc" command: cannot start command with \[ ... \] group`,
}}

func (s *S) TestParseCommand(c *C) {
	for _, test := range cmdTests {
		service := plan.Service{Name: "svc", Command: test.command}

		// parse base and the default arguments in [ ... ]
		base, extra, err := service.ParseCommand()
		if err != nil || test.error != "" {
			if test.error != "" {
				c.Assert(err, ErrorMatches, test.error)
			} else {
				c.Assert(err, IsNil)
			}
			continue
		}
		c.Assert(base, DeepEquals, test.expectedBase)
		c.Assert(extra, DeepEquals, test.expectedExtra)

		// add cmdArgs to base and produce a new command string
		newCommand := plan.CommandString(base, test.cmdArgs)
		c.Assert(newCommand, DeepEquals, test.expectedNewCommand)

		// parse the new command string again and check if base is
		// the same and cmdArgs is the new default arguments in [ ... ]
		service.Command = newCommand
		base, extra, err = service.ParseCommand()
		c.Assert(err, IsNil)
		c.Assert(base, DeepEquals, test.expectedBase)
		c.Assert(extra, DeepEquals, test.cmdArgs)
	}
}

func (s *S) TestLogsTo(c *C) {
	tests := []struct {
		services []string
		logsTo   map[string]bool
	}{{
		services: nil,
		logsTo: map[string]bool{
			"svc1": false,
			"svc2": false,
		},
	}, {
		services: []string{},
		logsTo: map[string]bool{
			"svc1": false,
			"svc2": false,
		},
	}, {
		services: []string{"all"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": true,
		},
	}, {
		services: []string{"svc1"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": false,
		},
	}, {
		services: []string{"svc1", "svc2"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": true,
			"svc3": false,
		},
	}, {
		services: []string{"all", "-svc2"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": false,
			"svc3": true,
		},
	}, {
		services: []string{"svc1", "svc2", "-svc1", "all"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": true,
			"svc3": true,
		},
	}, {
		services: []string{"svc1", "svc2", "-all"},
		logsTo: map[string]bool{
			"svc1": false,
			"svc2": false,
			"svc3": false,
		},
	}, {
		services: []string{"all", "-all"},
		logsTo: map[string]bool{
			"svc1": false,
			"svc2": false,
			"svc3": false,
		},
	}, {
		services: []string{"svc1", "svc2", "-all", "svc3", "svc1", "-svc3"},
		logsTo: map[string]bool{
			"svc1": true,
			"svc2": false,
			"svc3": false,
		},
	}}

	for _, test := range tests {
		target := &plan.LogTarget{
			Services: test.services,
		}

		for serviceName, shouldLogTo := range test.logsTo {
			service := &plan.Service{
				Name: serviceName,
			}
			c.Check(service.LogsTo(target), Equals, shouldLogTo,
				Commentf("matching service %q against 'services: %v'", serviceName, test.services))
		}
	}
}

func (s *S) TestMergeServiceContextNoContext(c *C) {
	userID, groupID := 10, 20
	overrides := plan.ContextOptions{
		Environment: map[string]string{"x": "y"},
		UserID:      &userID,
		User:        "usr",
		GroupID:     &groupID,
		Group:       "grp",
		WorkingDir:  "/working/dir",
	}
	// This test ensures an empty service name results in no lookup, and
	// simply leaves the provided context unchanged (if the nil CombinedPlan
	// is consulted, we would receive an error instead).
	merged, err := plan.MergeServiceContext(nil, "", overrides)
	c.Assert(err, IsNil)
	c.Check(merged, DeepEquals, overrides)
}

func (s *S) TestMergeServiceContextBadService(c *C) {
	p := plan.NewPlan()
	_, err := plan.MergeServiceContext(p.Combined, "nosvc", plan.ContextOptions{})
	c.Assert(err, ErrorMatches, `context service "nosvc" not found`)
}

func (s *S) TestMergeServiceContextNoOverrides(c *C) {
	userID, groupID := 11, 22
	layer := &plan.Layer{Services: map[string]*plan.Service{"svc1": {
		Name:        "svc1",
		Environment: map[string]string{"x": "y"},
		UserID:      &userID,
		User:        "svcuser",
		GroupID:     &groupID,
		Group:       "svcgroup",
		WorkingDir:  "/working/svc",
	}}}
	combinedPlan := plan.NewCombinedPlan(layer)
	merged, err := plan.MergeServiceContext(combinedPlan, "svc1", plan.ContextOptions{})
	c.Assert(err, IsNil)
	c.Check(merged, DeepEquals, plan.ContextOptions{
		Environment: map[string]string{"x": "y"},
		UserID:      &userID,
		User:        "svcuser",
		GroupID:     &groupID,
		Group:       "svcgroup",
		WorkingDir:  "/working/svc",
	})
}

func (s *S) TestMergeServiceContextOverrides(c *C) {
	svcUserID, svcGroupID := 10, 20
	layer := &plan.Layer{Services: map[string]*plan.Service{"svc1": {
		Name:        "svc1",
		Environment: map[string]string{"x": "y", "w": "z"},
		UserID:      &svcUserID,
		User:        "svcuser",
		GroupID:     &svcGroupID,
		Group:       "svcgroup",
		WorkingDir:  "/working/svc",
	}}}
	combinedPlan := plan.NewCombinedPlan(layer)
	userID, groupID := 11, 22
	overrides := plan.ContextOptions{
		Environment: map[string]string{"x": "a"},
		UserID:      &userID,
		User:        "usr",
		GroupID:     &groupID,
		Group:       "grp",
		WorkingDir:  "/working/dir",
	}
	merged, err := plan.MergeServiceContext(combinedPlan, "svc1", overrides)
	c.Assert(err, IsNil)
	c.Check(merged, DeepEquals, plan.ContextOptions{
		Environment: map[string]string{"x": "a", "w": "z"},
		UserID:      &userID,
		User:        "usr",
		GroupID:     &groupID,
		Group:       "grp",
		WorkingDir:  "/working/dir",
	})
}
