// Copyright (c) 2024 Canonical Ltd
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
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/testutil"
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
	error:   `cannot parse layer "layer-0" section "services": invalid duration "foo"`,
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
	error:   `cannot parse layer "layer-0" section "services": invalid floating-point number "foo"`,
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
		Sections:   map[string]plan.Section{},
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
				startup: enabled
				tcp:
					port: 7777
					host: somehost
		
			chk-exec:
				override: replace
				startup: disabled
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
				Startup:   plan.CheckStartupEnabled,
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
				Startup:   plan.CheckStartupDisabled,
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
	summary: `Invalid check startup value`,
	error:   `plan check "chk1" startup must be "enabled" or "disabled"`,
	input: []string{`
			checks:
				chk1:
					override: replace
					startup: true
					exec:
						command: foo
		`},
}, {}, {
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections: map[string]plan.Section{},
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
		Sections:   map[string]plan.Section{},
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
					lanes, err := p.StartOrder([]string{name})
					c.Assert(err, IsNil)
					for _, names := range lanes {
						if len(names) > 0 {
							c.Assert(names, DeepEquals, order)
						}
					}
				}
				for name, order := range test.stop {
					p := plan.Plan{Services: result.Services}
					lanes, err := p.StopOrder([]string{name})
					c.Assert(err, IsNil)
					for _, names := range lanes {
						if len(names) > 0 {
							c.Assert(names, DeepEquals, order)
						}
					}
				}
			}
			if err == nil {
				p := &plan.Plan{
					Layers:     sup.Layers,
					Services:   result.Services,
					Checks:     result.Checks,
					LogTargets: result.LogTargets,
					Sections:   result.Sections,
				}
				err = p.Validate()
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
	combined, err := plan.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	layers := []*plan.Layer{layer1, layer2}
	p := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
		Sections:   combined.Sections,
	}
	err = p.Validate()
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
	combined, err := plan.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	layers := []*plan.Layer{layer1, layer2}
	p := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
		Sections:   combined.Sections,
	}
	err = p.Validate()
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
	combined, err = plan.CombineLayers(layer1, layer2)
	c.Assert(err, IsNil)
	c.Assert(combined.Services["srv1"].Command, Equals, "foo --bar")
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
		sup, err := plan.ReadDir(layersDir)
		if err == nil {
			var result *plan.Layer
			result, err = plan.CombineLayers(sup.Layers...)
			if err == nil && test.result != nil {
				c.Assert(result, DeepEquals, test.result)
			}
			if err == nil {
				for name, order := range test.start {
					p := plan.Plan{Services: result.Services}
					lanes, err := p.StartOrder([]string{name})
					c.Assert(err, IsNil)
					for _, names := range lanes {
						if len(names) > 0 {
							c.Assert(names, DeepEquals, order)
						}
					}
				}
				for name, order := range test.stop {
					p := plan.Plan{Services: result.Services}
					lanes, err := p.StopOrder([]string{name})
					c.Assert(err, IsNil)
					for _, names := range lanes {
						if len(names) > 0 {
							c.Assert(names, DeepEquals, order)
						}
					}
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
		err := os.WriteFile(fpath, []byte("<ignore>"), 0644)
		c.Assert(err, IsNil)
		_, err = plan.ReadDir(layersDir)
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
		_, err = plan.ReadDir(layersDir)
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
	// simply leaves the provided context unchanged.
	merged, err := plan.MergeServiceContext(nil, "", overrides)
	c.Assert(err, IsNil)
	c.Check(merged, DeepEquals, overrides)
}

func (s *S) TestMergeServiceContextBadService(c *C) {
	_, err := plan.MergeServiceContext(&plan.Plan{}, "nosvc", plan.ContextOptions{})
	c.Assert(err, ErrorMatches, `context service "nosvc" not found`)
}

func (s *S) TestMergeServiceContextNoOverrides(c *C) {
	userID, groupID := 11, 22
	p := &plan.Plan{Services: map[string]*plan.Service{"svc1": {
		Name:        "svc1",
		Environment: map[string]string{"x": "y"},
		UserID:      &userID,
		User:        "svcuser",
		GroupID:     &groupID,
		Group:       "svcgroup",
		WorkingDir:  "/working/svc",
	}}}
	merged, err := plan.MergeServiceContext(p, "svc1", plan.ContextOptions{})
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
	p := &plan.Plan{Services: map[string]*plan.Service{"svc1": {
		Name:        "svc1",
		Environment: map[string]string{"x": "y", "w": "z"},
		UserID:      &svcUserID,
		User:        "svcuser",
		GroupID:     &svcGroupID,
		Group:       "svcgroup",
		WorkingDir:  "/working/svc",
	}}}
	userID, groupID := 11, 22
	overrides := plan.ContextOptions{
		Environment: map[string]string{"x": "a"},
		UserID:      &userID,
		User:        "usr",
		GroupID:     &groupID,
		Group:       "grp",
		WorkingDir:  "/working/dir",
	}
	merged, err := plan.MergeServiceContext(p, "svc1", overrides)
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

func (s *S) TestPebbleLabelPrefixReserved(c *C) {
	// Validate fails if layer label has the reserved prefix "pebble-"
	_, err := plan.ParseLayer(0, "pebble-foo", []byte("{}"))
	c.Check(err, ErrorMatches, `cannot use reserved label prefix "pebble-"`)
}

func (s *S) TestStartStopOrderSingleLane(c *C) {
	layer := &plan.Layer{
		Summary:     "services with dependencies in the same lane",
		Description: "a simple layer",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Override: "replace",
				Command:  `cmd`,
				Requires: []string{"srv2"},
				Before:   []string{"srv2"},
				Startup:  plan.StartupEnabled,
			},
			"srv2": {
				Name:     "srv2",
				Override: "replace",
				Command:  `cmd`,
				Requires: []string{"srv3"},
				Before:   []string{"srv3"},
				Startup:  plan.StartupEnabled,
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
	}

	p := plan.Plan{Services: layer.Services}

	lanes, err := p.StartOrder([]string{"srv1", "srv2", "srv3"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 1)
	c.Assert(lanes[0], DeepEquals, []string{"srv1", "srv2", "srv3"})

	lanes, err = p.StopOrder([]string{"srv1", "srv2", "srv3"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 1)
	c.Assert(lanes[0], DeepEquals, []string{"srv3", "srv2", "srv1"})
}

func (s *S) TestStartStopOrderMultipleLanes(c *C) {
	layer := &plan.Layer{
		Summary:     "services with no dependencies in different lanes",
		Description: "a simple layer",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
			"srv2": {
				Name:     "srv2",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
	}

	p := plan.Plan{Services: layer.Services}

	lanes, err := p.StartOrder([]string{"srv1", "srv2", "srv3"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 3)
	c.Assert(lanes[0], DeepEquals, []string{"srv1"})
	c.Assert(lanes[1], DeepEquals, []string{"srv2"})
	c.Assert(lanes[2], DeepEquals, []string{"srv3"})

	lanes, err = p.StopOrder([]string{"srv1", "srv2", "srv3"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 3)
	c.Assert(lanes[0], DeepEquals, []string{"srv1"})
	c.Assert(lanes[1], DeepEquals, []string{"srv2"})
	c.Assert(lanes[2], DeepEquals, []string{"srv3"})
}

func (s *S) TestStartStopOrderMultipleLanesRandomOrder(c *C) {
	layer := &plan.Layer{
		Summary:     "services with no dependencies in different lanes",
		Description: "a simple layer",
		Services: map[string]*plan.Service{
			"srv1": {
				Name:     "srv1",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
			"srv2": {
				Name:     "srv2",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
			"srv3": {
				Name:     "srv3",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
			},
			"srv4": {
				Name:     "srv4",
				Override: "replace",
				Command:  `cmd`,
				Startup:  plan.StartupEnabled,
				Requires: []string{"srv1"},
				After:    []string{"srv1"},
			},
		},
		Checks:     map[string]*plan.Check{},
		LogTargets: map[string]*plan.LogTarget{},
	}

	p := plan.Plan{Services: layer.Services}

	lanes, err := p.StartOrder([]string{"srv1", "srv2", "srv3", "srv4"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 3)
	c.Assert(lanes[0], DeepEquals, []string{"srv1", "srv4"})
	c.Assert(lanes[1], DeepEquals, []string{"srv2"})
	c.Assert(lanes[2], DeepEquals, []string{"srv3"})

	lanes, err = p.StopOrder([]string{"srv1", "srv2", "srv3", "srv4"})
	c.Assert(err, IsNil)
	c.Assert(len(lanes), Equals, 3)
	c.Assert(lanes[0], DeepEquals, []string{"srv4", "srv1"})
	c.Assert(lanes[1], DeepEquals, []string{"srv2"})
	c.Assert(lanes[2], DeepEquals, []string{"srv3"})
}

// TestSectionFieldStability detects changes in plan.Layer and plan.Plan
// YAML fields, and fails on any change that could break hardcoded section
// fields in the code. On failure, please carefully inspect and update
// the plan library where required.
func (s *S) TestSectionFieldStability(c *C) {
	layerFields := structYamlFields(plan.Layer{})
	c.Assert(layerFields, testutil.DeepUnsortedMatches, []string{"summary", "description", "services", "checks", "log-targets", "sections"})
	planFields := structYamlFields(plan.Plan{})
	c.Assert(planFields, testutil.DeepUnsortedMatches, []string{"services", "checks", "log-targets", "sections"})
}

// structYamlFields extracts the YAML fields from a struct. If the YAML tag
// is omitted, the field name with the first letter lower case will be used.
func structYamlFields(inStruct any) []string {
	var fields []string
	inStructType := reflect.TypeOf(inStruct)
	for i := range inStructType.NumField() {
		fieldType := inStructType.Field(i)
		yamlTag := fieldType.Tag.Get("yaml")
		if fieldType.IsExported() && yamlTag != "-" {
			tag, _, _ := strings.Cut(fieldType.Tag.Get("yaml"), ",")
			if tag == "" {
				tag = firstLetterToLower(fieldType.Name)
			}
			fields = append(fields, tag)
		}
	}
	return fields
}

func firstLetterToLower(s string) string {
	if len(s) == 0 {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// TestSectionOrder ensures built-in section order is maintained
// during Plan marshal operations.
func (s *S) TestSectionOrder(c *C) {
	layer, err := plan.ParseLayer(1, "label", reindent(`
	checks:
		chk1:
			override: replace
			exec:
				command: ping 8.8.8.8
	log-targets:
		lt1:
			override: replace
			type: loki
			location: http://192.168.1.2:3100/loki/api/v1/push
	services:
		srv1:
			override: replace
			command: cmd`))
	c.Assert(err, IsNil)
	combined, err := plan.CombineLayers(layer)
	c.Assert(err, IsNil)
	plan := plan.Plan{
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	data, err := yaml.Marshal(plan)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, string(reindent(`
	services:
		srv1:
			override: replace
			command: cmd
	checks:
		chk1:
			override: replace
			threshold: 3
			exec:
				command: ping 8.8.8.8
	log-targets:
		lt1:
			type: loki
			location: http://192.168.1.2:3100/loki/api/v1/push
			services: []
			override: replace`)))
}

// createLayerPath combines the base path with a configuration layer
// filename (which may include a single sub-directory), and creates
// the missing directories and an empty configuration file.
func createLayerPath(c *C, base string, name string) {
	path := filepath.Join(base, name)
	err := os.MkdirAll(filepath.Dir(path), 0777)
	c.Assert(err, IsNil)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	c.Assert(err, IsNil)
	// Let's mix in the layer name into the command so we can
	// verify the correct layer has the correct file content.
	_, err = file.Write(reindent(fmt.Sprintf(`
	services:
		srv:
			override: replace
			command: %s`, name)))
	c.Assert(err, IsNil)
	err = file.Close()
	c.Assert(err, IsNil)
}

var readDirTests = []struct {
	summary    string
	layerNames []string
	orders     []int
	labels     []string
	error      string
}{{
	summary: "Invalid filename #1",
	layerNames: []string{
		"001foo.yaml",
	},
	error: ".*invalid layer filename.*",
}, {
	summary: "Invalid filename #2",
	layerNames: []string{
		"001-foo.d/001foo.yaml",
	},
	error: ".*invalid layer filename.*",
}, {
	summary: "Invalid sub-directory",
	layerNames: []string{
		"001foo.d/001-bar.yaml",
	},
	error: ".*invalid layer sub.*",
}, {
	summary: "Not unique order #1",
	layerNames: []string{
		"001-foo.yaml",
		"001-bar.yaml",
	},
	error: ".*not unique.*",
}, {
	summary: "Not unique order #2",
	layerNames: []string{
		"002-dir.d/001-foo.yaml",
		"002-dir.d/001-bar.yaml",
	},
	error: ".*not unique.*",
}, {
	summary: "Not unique label #1",
	layerNames: []string{
		"001-foo.yaml",
		"002-foo.yaml",
	},
	error: ".*not unique.*",
}, {
	summary: "Not unique label #2",
	layerNames: []string{
		"002-dir.d/001-foo.yaml",
		"002-dir.d/002-foo.yaml",
	},
	error: ".*not unique.*",
}, {
	summary: "Valid load",
	layerNames: []string{
		"001-aaa.yaml",
		"010-bbb.yaml",
		"002-dir.d/100-foo.yaml",
		"002-dir.d/090-bar.yaml",
		"008-ccc.yaml",
		"900-overlay.d/002-something.yaml",
		"900-overlay.d/001-else.yaml",
		"003-plans.d/999-final.yaml",
		"009-baz.d/009-baz.yaml",
	},
	orders: []int{
		1000,
		2090,
		2100,
		3999,
		8000,
		9009,
		10000,
		900001,
		900002,
	},
	labels: []string{
		"aaa",
		"dir/bar",
		"dir/foo",
		"plans/final",
		"ccc",
		"baz/baz",
		"bbb",
		"overlay/else",
		"overlay/something",
	},
}}

func (s *S) TestReadLayersDir(c *C) {
	for _, test := range readDirTests {
		c.Logf("Running ReadLayersDir: %s", test.summary)

		tempDir := c.MkDir()

		for _, layerName := range test.layerNames {
			createLayerPath(c, tempDir, layerName)
		}

		layers, err := plan.ReadLayersDir(tempDir)
		if test.error != "" || err != nil {
			c.Assert(err, ErrorMatches, test.error)
		}

		c.Assert(len(layers), Equals, len(test.labels))
		c.Assert(len(layers), Equals, len(test.orders))

		for i, layer := range layers {
			c.Assert(layer.Order, Equals, test.orders[i])
			c.Assert(layer.Label, Equals, test.labels[i])

			// Let's make sure each file contains the expected
			// command. This will confirm the content is loaded
			// from the correct file.
			ordered := make([]string, 0, len(test.layerNames))
			ordered = append(ordered, test.layerNames...)
			slices.Sort(ordered)

			c.Assert(layer.Services, DeepEquals, map[string]*plan.Service{
				"srv": {
					Name:     "srv",
					Override: "replace",
					Command:  ordered[i],
				},
			})
		}
	}
}
