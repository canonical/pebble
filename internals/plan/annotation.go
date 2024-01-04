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

package plan

import (
	"gopkg.in/yaml.v3"
)

type PlanYaml struct {
	Services   map[string]*ServiceYaml   `yaml:"services,omitempty"`
	Checks     map[string]*CheckYaml     `yaml:"checks,omitempty"`
	LogTargets map[string]*LogTargetYaml `yaml:"log-targets,omitempty"`
}

type ServiceYaml struct {
	Summary        yaml.Node            `yaml:"summary,omitempty"`
	Description    yaml.Node            `yaml:"description,omitempty"`
	Startup        yaml.Node            `yaml:"startup,omitempty"`
	Override       yaml.Node            `yaml:"override,omitempty"`
	Command        yaml.Node            `yaml:"command,omitempty"`
	After          []yaml.Node          `yaml:"after,omitempty"`
	Before         []yaml.Node          `yaml:"before,omitempty"`
	Requires       []yaml.Node          `yaml:"requires,omitempty"`
	Environment    map[string]yaml.Node `yaml:"environment,omitempty"`
	UserID         yaml.Node            `yaml:"user-id,omitempty"`
	User           yaml.Node            `yaml:"user,omitempty"`
	GroupID        yaml.Node            `yaml:"group-id,omitempty"`
	Group          yaml.Node            `yaml:"group,omitempty"`
	WorkingDir     yaml.Node            `yaml:"working-dir,omitempty"`
	OnSuccess      yaml.Node            `yaml:"on-success,omitempty"`
	OnFailure      yaml.Node            `yaml:"on-failure,omitempty"`
	OnCheckFailure map[string]yaml.Node `yaml:"on-check-failure,omitempty"`
	BackoffDelay   yaml.Node            `yaml:"backoff-delay,omitempty"`
	BackoffFactor  yaml.Node            `yaml:"backoff-factor,omitempty"`
	BackoffLimit   yaml.Node            `yaml:"backoff-limit,omitempty"`
	KillDelay      yaml.Node            `yaml:"kill-delay,omitempty"`
}

type CheckYaml struct {
	Override  yaml.Node      `yaml:"override,omitempty"`
	Level     yaml.Node      `yaml:"level,omitempty"`
	Period    yaml.Node      `yaml:"period,omitempty"`
	Timeout   yaml.Node      `yaml:"timeout,omitempty"`
	Threshold yaml.Node      `yaml:"threshold,omitempty"`
	HTTP      *HTTPCheckYaml `yaml:"http,omitempty"`
	TCP       *TCPCheckYaml  `yaml:"tcp,omitempty"`
	Exec      *ExecCheckYaml `yaml:"exec,omitempty"`
}

type LogTargetYaml struct {
	Type     yaml.Node            `yaml:"type"`
	Location yaml.Node            `yaml:"location"`
	Services []yaml.Node          `yaml:"services"`
	Override yaml.Node            `yaml:"override,omitempty"`
	Labels   map[string]yaml.Node `yaml:"labels,omitempty"`
}

type HTTPCheckYaml struct {
	URL     yaml.Node            `yaml:"url,omitempty"`
	Headers map[string]yaml.Node `yaml:"headers,omitempty"`
}

type ExecCheckYaml struct {
	Command        yaml.Node            `yaml:"command,omitempty"`
	ServiceContext yaml.Node            `yaml:"service-context,omitempty"`
	Environment    map[string]yaml.Node `yaml:"environment,omitempty"`
	UserID         yaml.Node            `yaml:"user-id,omitempty"`
	User           yaml.Node            `yaml:"user,omitempty"`
	GroupID        yaml.Node            `yaml:"group-id,omitempty"`
	Group          yaml.Node            `yaml:"group,omitempty"`
	WorkingDir     yaml.Node            `yaml:"working-dir,omitempty"`
}

type TCPCheckYaml struct {
	Port yaml.Node `yaml:"port,omitempty"`
	Host yaml.Node `yaml:"host,omitempty"`
}
