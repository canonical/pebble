// Copyright (c) 2021 Canonical Ltd
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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/x-go/strutil/shlex"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internal/osutil"
)

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second

	defaultCheckPeriod    = 10 * time.Second
	defaultCheckTimeout   = 3 * time.Second
	defaultCheckThreshold = 3
)

type Plan struct {
	Layers     []*Layer              `yaml:"-"`
	Services   map[string]*Service   `yaml:"services,omitempty"`
	Checks     map[string]*Check     `yaml:"checks,omitempty"`
	LogTargets map[string]*LogTarget `yaml:"log-targets,omitempty"`
}

type Layer struct {
	Order       int                   `yaml:"-"`
	Label       string                `yaml:"-"`
	Summary     string                `yaml:"summary,omitempty"`
	Description string                `yaml:"description,omitempty"`
	Services    map[string]*Service   `yaml:"services,omitempty"`
	Checks      map[string]*Check     `yaml:"checks,omitempty"`
	LogTargets  map[string]*LogTarget `yaml:"log-targets,omitempty"`
}

type Service struct {
	// Basic details
	Name        string         `yaml:"-"`
	Summary     string         `yaml:"summary,omitempty"`
	Description string         `yaml:"description,omitempty"`
	Startup     ServiceStartup `yaml:"startup,omitempty"`
	Override    Override       `yaml:"override,omitempty"`
	Command     string         `yaml:"command,omitempty"`

	// Service dependencies
	After    []string `yaml:"after,omitempty"`
	Before   []string `yaml:"before,omitempty"`
	Requires []string `yaml:"requires,omitempty"`

	// Options for command execution
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`

	// Auto-restart and backoff functionality
	OnSuccess      ServiceAction            `yaml:"on-success,omitempty"`
	OnFailure      ServiceAction            `yaml:"on-failure,omitempty"`
	OnCheckFailure map[string]ServiceAction `yaml:"on-check-failure,omitempty"`
	BackoffDelay   OptionalDuration         `yaml:"backoff-delay,omitempty"`
	BackoffFactor  OptionalFloat            `yaml:"backoff-factor,omitempty"`
	BackoffLimit   OptionalDuration         `yaml:"backoff-limit,omitempty"`
	KillDelay      OptionalDuration         `yaml:"kill-delay,omitempty"`

	// Log forwarding
	LogTargets []string `yaml:"log-targets,omitempty"`
}

// Copy returns a deep copy of the service.
func (s *Service) Copy() *Service {
	copied := *s
	copied.After = append([]string(nil), s.After...)
	copied.Before = append([]string(nil), s.Before...)
	copied.Requires = append([]string(nil), s.Requires...)
	if s.Environment != nil {
		copied.Environment = make(map[string]string)
		for k, v := range s.Environment {
			copied.Environment[k] = v
		}
	}
	if s.UserID != nil {
		userID := *s.UserID
		copied.UserID = &userID
	}
	if s.GroupID != nil {
		groupID := *s.GroupID
		copied.GroupID = &groupID
	}
	if s.OnCheckFailure != nil {
		copied.OnCheckFailure = make(map[string]ServiceAction)
		for k, v := range s.OnCheckFailure {
			copied.OnCheckFailure[k] = v
		}
	}
	copied.LogTargets = append([]string(nil), s.LogTargets...)
	return &copied
}

// Merge merges the fields set in other into s.
func (s *Service) Merge(other *Service) {
	if other.Summary != "" {
		s.Summary = other.Summary
	}
	if other.Description != "" {
		s.Description = other.Description
	}
	if other.Startup != StartupUnknown {
		s.Startup = other.Startup
	}
	if other.Command != "" {
		s.Command = other.Command
	}
	if other.KillDelay.IsSet {
		s.KillDelay = other.KillDelay
	}
	if other.UserID != nil {
		userID := *other.UserID
		s.UserID = &userID
	}
	if other.User != "" {
		s.User = other.User
	}
	if other.GroupID != nil {
		groupID := *other.GroupID
		s.GroupID = &groupID
	}
	if other.Group != "" {
		s.Group = other.Group
	}
	s.After = append(s.After, other.After...)
	s.Before = append(s.Before, other.Before...)
	s.Requires = append(s.Requires, other.Requires...)
	for k, v := range other.Environment {
		if s.Environment == nil {
			s.Environment = make(map[string]string)
		}
		s.Environment[k] = v
	}
	if other.OnSuccess != "" {
		s.OnSuccess = other.OnSuccess
	}
	if other.OnFailure != "" {
		s.OnFailure = other.OnFailure
	}
	for k, v := range other.OnCheckFailure {
		if s.OnCheckFailure == nil {
			s.OnCheckFailure = make(map[string]ServiceAction)
		}
		s.OnCheckFailure[k] = v
	}
	if other.BackoffDelay.IsSet {
		s.BackoffDelay = other.BackoffDelay
	}
	if other.BackoffFactor.IsSet {
		s.BackoffFactor = other.BackoffFactor
	}
	if other.BackoffLimit.IsSet {
		s.BackoffLimit = other.BackoffLimit
	}
	s.LogTargets = appendUnique(s.LogTargets, other.LogTargets...)
}

// appendUnique appends into a the elements from b which are not yet present
// and returns the modified slice.
// TODO: move this function into canonical/x-go/strutil
func appendUnique(a []string, b ...string) []string {
Outer:
	for _, bn := range b {
		for _, an := range a {
			if an == bn {
				continue Outer
			}
		}
		a = append(a, bn)
	}
	return a
}

// Equal returns true when the two services are equal in value.
func (s *Service) Equal(other *Service) bool {
	if s == other {
		return true
	}
	return reflect.DeepEqual(s, other)
}

// ParseCommand returns a service command as two stream of strings.
// The base command is returned as a stream and the default arguments
// in [ ... ] group is returned as another stream.
func (s *Service) ParseCommand() (base, extra []string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("cannot parse service %q command: %w", s.Name, err)
		}
	}()

	args, err := shlex.Split(s.Command)
	if err != nil {
		return nil, nil, err
	}

	var inBrackets, gotBrackets bool

	for idx, arg := range args {
		if inBrackets {
			if arg == "[" {
				return nil, nil, fmt.Errorf("cannot nest [ ... ] groups")
			}
			if arg == "]" {
				inBrackets = false
				continue
			}
			extra = append(extra, arg)
			continue
		}
		if gotBrackets {
			return nil, nil, fmt.Errorf("cannot have any arguments after [ ... ] group")
		}
		if arg == "[" {
			if idx == 0 {
				return nil, nil, fmt.Errorf("cannot start command with [ ... ] group")
			}
			inBrackets = true
			gotBrackets = true
			continue
		}
		if arg == "]" {
			return nil, nil, fmt.Errorf("cannot have ] outside of [ ... ] group")
		}
		base = append(base, arg)
	}

	return base, extra, nil
}

// CommandString returns a service command as a string after
// appending the arguments in "extra" to the command in "base"
func CommandString(base, extra []string) string {
	output := shlex.Join(base)
	if len(extra) > 0 {
		output = output + " [ " + shlex.Join(extra) + " ]"
	}
	return output
}

// LogsTo returns true if the logs from s should be forwarded to target t.
// This happens if:
//   - t.Selection is "opt-out" or empty, and s.LogTargets is empty; or
//   - t.Selection is not "disabled", and s.LogTargets contains t.
func (s *Service) LogsTo(t *LogTarget) bool {
	if t.Selection == DisabledSelection {
		return false
	}
	if len(s.LogTargets) == 0 {
		if t.Selection == UnsetSelection || t.Selection == OptOutSelection {
			return true
		}
	}
	for _, targetName := range s.LogTargets {
		if targetName == t.Name {
			return true
		}
	}
	return false
}

type ServiceStartup string

const (
	StartupUnknown  ServiceStartup = ""
	StartupEnabled  ServiceStartup = "enabled"
	StartupDisabled ServiceStartup = "disabled"
)

// Override specifies the layer override mechanism for an object.
type Override string

const (
	UnknownOverride Override = ""
	MergeOverride   Override = "merge"
	ReplaceOverride Override = "replace"
)

type ServiceAction string

const (
	ActionUnset    ServiceAction = ""
	ActionRestart  ServiceAction = "restart"
	ActionShutdown ServiceAction = "shutdown"
	ActionIgnore   ServiceAction = "ignore"
)

// Check specifies configuration for a single health check.
type Check struct {
	// Basic details
	Name     string     `yaml:"-"`
	Override Override   `yaml:"override,omitempty"`
	Level    CheckLevel `yaml:"level,omitempty"`

	// Common check settings
	Period    OptionalDuration `yaml:"period,omitempty"`
	Timeout   OptionalDuration `yaml:"timeout,omitempty"`
	Threshold int              `yaml:"threshold,omitempty"`

	// Type-specific check settings (only one of these can be set)
	HTTP *HTTPCheck `yaml:"http,omitempty"`
	TCP  *TCPCheck  `yaml:"tcp,omitempty"`
	Exec *ExecCheck `yaml:"exec,omitempty"`
}

// Copy returns a deep copy of the check configuration.
func (c *Check) Copy() *Check {
	copied := *c
	if c.HTTP != nil {
		copied.HTTP = c.HTTP.Copy()
	}
	if c.TCP != nil {
		copied.TCP = c.TCP.Copy()
	}
	if c.Exec != nil {
		copied.Exec = c.Exec.Copy()
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *Check) Merge(other *Check) {
	if other.Level != "" {
		c.Level = other.Level
	}
	if other.Period.IsSet {
		c.Period = other.Period
	}
	if other.Timeout.IsSet {
		c.Timeout = other.Timeout
	}
	if other.Threshold != 0 {
		c.Threshold = other.Threshold
	}
	if other.HTTP != nil {
		if c.HTTP == nil {
			c.HTTP = &HTTPCheck{}
		}
		c.HTTP.Merge(other.HTTP)
	}
	if other.TCP != nil {
		if c.TCP == nil {
			c.TCP = &TCPCheck{}
		}
		c.TCP.Merge(other.TCP)
	}
	if other.Exec != nil {
		if c.Exec == nil {
			c.Exec = &ExecCheck{}
		}
		c.Exec.Merge(other.Exec)
	}
}

// CheckLevel specifies the optional check level.
type CheckLevel string

const (
	UnsetLevel CheckLevel = ""
	AliveLevel CheckLevel = "alive"
	ReadyLevel CheckLevel = "ready"
)

// HTTPCheck holds the configuration for an HTTP health check.
type HTTPCheck struct {
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// Copy returns a deep copy of the HTTP check configuration.
func (c *HTTPCheck) Copy() *HTTPCheck {
	copied := *c
	if c.Headers != nil {
		copied.Headers = make(map[string]string, len(c.Headers))
		for k, v := range c.Headers {
			copied.Headers[k] = v
		}
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *HTTPCheck) Merge(other *HTTPCheck) {
	if other.URL != "" {
		c.URL = other.URL
	}
	for k, v := range other.Headers {
		if c.Headers == nil {
			c.Headers = make(map[string]string)
		}
		c.Headers[k] = v
	}
}

// TCPCheck holds the configuration for an HTTP health check.
type TCPCheck struct {
	Port int    `yaml:"port,omitempty"`
	Host string `yaml:"host,omitempty"`
}

// Copy returns a deep copy of the TCP check configuration.
func (c *TCPCheck) Copy() *TCPCheck {
	copied := *c
	return &copied
}

// Merge merges the fields set in other into c.
func (c *TCPCheck) Merge(other *TCPCheck) {
	if other.Port != 0 {
		c.Port = other.Port
	}
	if other.Host != "" {
		c.Host = other.Host
	}
}

// ExecCheck holds the configuration for an exec health check.
type ExecCheck struct {
	Command     string            `yaml:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`
	WorkingDir  string            `yaml:"working-dir,omitempty"`
}

// Copy returns a deep copy of the exec check configuration.
func (c *ExecCheck) Copy() *ExecCheck {
	copied := *c
	if c.Environment != nil {
		copied.Environment = make(map[string]string, len(c.Environment))
		for k, v := range c.Environment {
			copied.Environment[k] = v
		}
	}
	if c.UserID != nil {
		userID := *c.UserID
		copied.UserID = &userID
	}
	if c.GroupID != nil {
		groupID := *c.GroupID
		copied.GroupID = &groupID
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *ExecCheck) Merge(other *ExecCheck) {
	if other.Command != "" {
		c.Command = other.Command
	}
	for k, v := range other.Environment {
		if c.Environment == nil {
			c.Environment = make(map[string]string)
		}
		c.Environment[k] = v
	}
	if other.UserID != nil {
		userID := *other.UserID
		c.UserID = &userID
	}
	if other.User != "" {
		c.User = other.User
	}
	if other.GroupID != nil {
		groupID := *other.GroupID
		c.GroupID = &groupID
	}
	if other.Group != "" {
		c.Group = other.Group
	}
	if other.WorkingDir != "" {
		c.WorkingDir = other.WorkingDir
	}
}

// LogTarget specifies a remote server to forward logs to.
type LogTarget struct {
	Name      string        `yaml:"-"`
	Type      LogTargetType `yaml:"type"`
	Location  string        `yaml:"location"`
	Selection Selection     `yaml:"selection,omitempty"`
	Override  Override      `yaml:"override,omitempty"`
}

// LogTargetType defines the protocol to use to forward logs.
type LogTargetType string

const (
	LokiTarget     LogTargetType = "loki"
	SyslogTarget   LogTargetType = "syslog"
	UnsetLogTarget LogTargetType = ""
)

// Selection describes which services' logs will be forwarded to this target.
type Selection string

const (
	OptOutSelection   Selection = "opt-out"
	OptInSelection    Selection = "opt-in"
	DisabledSelection Selection = "disabled"
	UnsetSelection    Selection = ""
)

// Copy returns a deep copy of the log target configuration.
func (t *LogTarget) Copy() *LogTarget {
	copied := *t
	return &copied
}

// Merge merges the fields set in other into t.
func (t *LogTarget) Merge(other *LogTarget) {
	if other.Type != "" {
		t.Type = other.Type
	}
	if other.Location != "" {
		t.Location = other.Location
	}
	if other.Selection != "" {
		t.Selection = other.Selection
	}
}

// FormatError is the error returned when a layer has a format error, such as
// a missing "override" field.
type FormatError struct {
	Message string
}

func (e *FormatError) Error() string {
	return e.Message
}

// CombineLayers combines the given layers into a single layer, with the later
// layers overriding earlier ones.
func CombineLayers(layers ...*Layer) (*Layer, error) {
	combined := &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
	}
	if len(layers) == 0 {
		return combined, nil
	}
	last := layers[len(layers)-1]
	combined.Summary = last.Summary
	combined.Description = last.Description
	for _, layer := range layers {
		for name, service := range layer.Services {
			switch service.Override {
			case MergeOverride:
				if old, ok := combined.Services[name]; ok {
					copied := old.Copy()
					copied.Merge(service)
					combined.Services[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.Services[name] = service.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for service %q`,
						layer.Label, service.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for service %q`,
						layer.Label, service.Name),
				}
			}
		}

		for name, check := range layer.Checks {
			switch check.Override {
			case MergeOverride:
				if old, ok := combined.Checks[name]; ok {
					copied := old.Copy()
					copied.Merge(check)
					combined.Checks[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.Checks[name] = check.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for check %q`,
						layer.Label, check.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for check %q`,
						layer.Label, check.Name),
				}
			}
		}

		for name, target := range layer.LogTargets {
			switch target.Override {
			case MergeOverride:
				if old, ok := combined.LogTargets[name]; ok {
					copied := old.Copy()
					copied.Merge(target)
					combined.LogTargets[name] = copied
					break
				}
				fallthrough
			case ReplaceOverride:
				combined.LogTargets[name] = target.Copy()
			case UnknownOverride:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q must define "override" for log target %q`,
						layer.Label, target.Name),
				}
			default:
				return nil, &FormatError{
					Message: fmt.Sprintf(`layer %q has invalid "override" value for log target %q`,
						layer.Label, target.Name),
				}
			}
		}
	}

	// Ensure fields in combined layers validate correctly (and set defaults).
	for name, service := range combined.Services {
		if service.Command == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "command" for service %q`, name),
			}
		}
		_, _, err := service.ParseCommand()
		if err != nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q command invalid: %v", name, err),
			}
		}
		if !validServiceAction(service.OnSuccess) {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q on-success action %q invalid", name, service.OnSuccess),
			}
		}
		if !validServiceAction(service.OnFailure) {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q on-failure action %q invalid", name, service.OnFailure),
			}
		}
		for _, action := range service.OnCheckFailure {
			if !validServiceAction(action) {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan service %q on-check-failure action %q invalid", name, action),
				}
			}
		}
		if !service.BackoffDelay.IsSet {
			service.BackoffDelay.Value = defaultBackoffDelay
		}
		if !service.BackoffFactor.IsSet {
			service.BackoffFactor.Value = defaultBackoffFactor
		} else if service.BackoffFactor.Value < 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan service %q backoff-factor must be 1.0 or greater, not %g", name, service.BackoffFactor.Value),
			}
		}
		if !service.BackoffLimit.IsSet {
			service.BackoffLimit.Value = defaultBackoffLimit
		}

	}

	for name, check := range combined.Checks {
		if check.Level != UnsetLevel && check.Level != AliveLevel && check.Level != ReadyLevel {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan check %q level must be "alive" or "ready"`, name),
			}
		}
		if !check.Period.IsSet {
			check.Period.Value = defaultCheckPeriod
		} else if check.Period.Value == 0 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q period must not be zero", name),
			}
		}
		if !check.Timeout.IsSet {
			check.Timeout.Value = defaultCheckTimeout
		} else if check.Timeout.Value == 0 {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must not be zero", name),
			}
		} else if check.Timeout.Value >= check.Period.Value {
			return nil, &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must be less than period", name),
			}
		}
		if check.Threshold == 0 {
			// Default number of failures in a row before check triggers
			// action, default is >1 to avoid flapping due to glitches. For
			// what it's worth, Kubernetes probes uses a default of 3 too.
			check.Threshold = defaultCheckThreshold
		}

		numTypes := 0
		if check.HTTP != nil {
			if check.HTTP.URL == "" {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "url" for http check %q`, name),
				}
			}
			numTypes++
		}
		if check.TCP != nil {
			if check.TCP.Port == 0 {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "port" for tcp check %q`, name),
				}
			}
			numTypes++
		}
		if check.Exec != nil {
			if check.Exec.Command == "" {
				return nil, &FormatError{
					Message: fmt.Sprintf(`plan must set "command" for exec check %q`, name),
				}
			}
			_, err := shlex.Split(check.Exec.Command)
			if err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan check %q command invalid: %v", name, err),
				}
			}
			_, _, err = osutil.NormalizeUidGid(check.Exec.UserID, check.Exec.GroupID, check.Exec.User, check.Exec.Group)
			if err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("plan check %q has invalid user/group: %v", name, err),
				}
			}
			numTypes++
		}
		if numTypes != 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must specify one of "http", "tcp", or "exec" for check %q`, name),
			}
		}
	}

	for name, target := range combined.LogTargets {
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "type" (%q or %q) for log target %q`,
					LokiTarget, SyslogTarget, name),
			}
		default:
			return nil, &FormatError{
				Message: fmt.Sprintf(`log target %q has unsupported type %q, must be %q or %q`,
					name, target.Type, LokiTarget, SyslogTarget),
			}
		}

		switch target.Selection {
		case OptOutSelection, OptInSelection, DisabledSelection, UnsetSelection:
			// valid, continue
		default:
			return nil, &FormatError{
				Message: fmt.Sprintf(`log target %q has invalid selection %q, must be %q, %q or %q`,
					name, target.Selection, OptOutSelection, OptInSelection, DisabledSelection),
			}
		}

		if target.Location == "" && target.Selection != DisabledSelection {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "location" for log target %q`, name),
			}
		}
	}

	// Validate service log targets
	for serviceName, service := range combined.Services {
		for _, targetName := range service.LogTargets {
			_, ok := combined.LogTargets[targetName]
			if !ok {
				return nil, &FormatError{
					Message: fmt.Sprintf(`unknown log target %q for service %q`, targetName, serviceName),
				}
			}
		}
	}

	// Ensure combined layers don't have cycles.
	err := combined.checkCycles()
	if err != nil {
		return nil, err
	}

	return combined, nil
}

// StartOrder returns the required services that must be started for the named
// services to be properly started, in the order that they must be started.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StartOrder(names []string) ([]string, error) {
	return order(p.Services, names, false)
}

// StopOrder returns the required services that must be stopped for the named
// services to be properly stopped, in the order that they must be stopped.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StopOrder(names []string) ([]string, error) {
	return order(p.Services, names, true)
}

func order(services map[string]*Service, names []string, stop bool) ([]string, error) {
	// For stop, create a list of reversed dependencies.
	predecessors := map[string][]string(nil)
	if stop {
		predecessors = make(map[string][]string)
		for name, service := range services {
			for _, req := range service.Requires {
				predecessors[req] = append(predecessors[req], name)
			}
		}
	}

	// Collect all services that will be started or stopped.
	successors := map[string][]string{}
	pending := append([]string(nil), names...)
	for i := 0; i < len(pending); i++ {
		name := pending[i]
		if _, seen := successors[name]; seen {
			continue
		}
		successors[name] = nil
		if stop {
			pending = append(pending, predecessors[name]...)
		} else {
			service, ok := services[name]
			if !ok {
				return nil, &FormatError{
					Message: fmt.Sprintf("service %q does not exist", name),
				}
			}
			pending = append(pending, service.Requires...)
		}
	}

	// Create a list of successors involving those services only.
	for name := range successors {
		service, ok := services[name]
		if !ok {
			return nil, &FormatError{
				Message: fmt.Sprintf("service %q does not exist", name),
			}
		}
		succs := successors[name]
		serviceAfter := service.After
		serviceBefore := service.Before
		if stop {
			serviceAfter, serviceBefore = serviceBefore, serviceAfter
		}
		for _, after := range serviceAfter {
			if _, required := successors[after]; required {
				succs = append(succs, after)
			}
		}
		successors[name] = succs
		for _, before := range serviceBefore {
			if succs, required := successors[before]; required {
				successors[before] = append(succs, name)
			}
		}
	}

	// Sort them up.
	var order []string
	for _, names := range tarjanSort(successors) {
		if len(names) > 1 {
			return nil, &FormatError{
				Message: fmt.Sprintf("services in before/after loop: %s", strings.Join(names, ", ")),
			}
		}
		order = append(order, names[0])
	}
	return order, nil
}

func (l *Layer) checkCycles() error {
	var names []string
	for name := range l.Services {
		names = append(names, name)
	}
	_, err := order(l.Services, names, false)
	return err
}

func ParseLayer(order int, label string, data []byte) (*Layer, error) {
	layer := Layer{
		Services:   map[string]*Service{},
		Checks:     map[string]*Check{},
		LogTargets: map[string]*LogTarget{},
	}
	dec := yaml.NewDecoder(bytes.NewBuffer(data))
	dec.KnownFields(true)
	err := dec.Decode(&layer)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}
	layer.Order = order
	layer.Label = label

	for name, service := range layer.Services {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as service name"),
			}
		}
		if name == "pebble" {
			// Disallow service name "pebble" to avoid ambiguity (for example,
			// in log output).
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use reserved service name %q", name),
			}
		}
		if service == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("service object cannot be null for service %q", name),
			}
		}
		service.Name = name
	}

	for name, check := range layer.Checks {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as check name"),
			}
		}
		if check == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("check object cannot be null for check %q", name),
			}
		}
		check.Name = name
	}

	for name, target := range layer.LogTargets {
		if name == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot use empty string as log target name"),
			}
		}
		if target == nil {
			return nil, &FormatError{
				Message: fmt.Sprintf("log target object cannot be null for log target %q", name),
			}
		}
		target.Name = name
	}

	err = layer.checkCycles()
	if err != nil {
		return nil, err
	}
	return &layer, err
}

func validServiceAction(action ServiceAction) bool {
	switch action {
	case ActionUnset, ActionRestart, ActionShutdown, ActionIgnore:
		return true
	default:
		return false
	}
}

var fnameExp = regexp.MustCompile("^([0-9]{3})-([a-z](?:-?[a-z0-9]){2,}).yaml$")

func ReadLayersDir(dirname string) ([]*Layer, error) {
	finfos, err := ioutil.ReadDir(dirname)
	if err != nil {
		// Errors from package os generally include the path.
		return nil, fmt.Errorf("cannot read layers directory: %v", err)
	}

	orders := make(map[int]string)
	labels := make(map[string]int)

	// Documentation says ReadDir result is already sorted by name.
	// This is fundamental here so if reading changes make sure the
	// sorting is preserved.
	var layers []*Layer
	for _, finfo := range finfos {
		if finfo.IsDir() || !strings.HasSuffix(finfo.Name(), ".yaml") {
			continue
		}
		// TODO Consider enforcing permissions and ownership here to
		//      avoid mistakes that could lead to hacks.
		match := fnameExp.FindStringSubmatch(finfo.Name())
		if match == nil {
			return nil, fmt.Errorf("invalid layer filename: %q (must look like \"123-some-label.yaml\")", finfo.Name())
		}

		data, err := ioutil.ReadFile(filepath.Join(dirname, finfo.Name()))
		if err != nil {
			// Errors from package os generally include the path.
			return nil, fmt.Errorf("cannot read layer file: %v", err)
		}
		label := match[2]
		order, err := strconv.Atoi(match[1])
		if err != nil {
			panic(fmt.Sprintf("internal error: filename regexp is wrong: %v", err))
		}

		oldLabel, dupOrder := orders[order]
		oldOrder, dupLabel := labels[label]
		if dupOrder {
			oldOrder = order
		} else if dupLabel {
			oldLabel = label
		}
		if dupOrder || dupLabel {
			return nil, fmt.Errorf("invalid layer filename: %q not unique (have \"%03d-%s.yaml\" already)", finfo.Name(), oldOrder, oldLabel)
		}

		orders[order] = label
		labels[label] = order

		layer, err := ParseLayer(order, label, data)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// ReadDir reads the configuration layers from the "layers" sub-directory in
// dir, and returns the resulting Plan. If the "layers" sub-directory doesn't
// exist, it returns a valid Plan with no layers.
func ReadDir(dir string) (*Plan, error) {
	layersDir := filepath.Join(dir, "layers")
	_, err := os.Stat(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Plan{}, nil
		}
		return nil, err
	}

	layers, err := ReadLayersDir(layersDir)
	if err != nil {
		return nil, err
	}
	combined, err := CombineLayers(layers...)
	if err != nil {
		return nil, err
	}
	plan := &Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	return plan, err
}
