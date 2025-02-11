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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/x-go/strutil/shlex"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
)

// SectionExtension allows the plan layer schema to be extended without
// adding centralised schema knowledge to the plan library.
type SectionExtension interface {
	// ParseSection returns a newly allocated concrete type containing the
	// unmarshalled section content.
	ParseSection(data yaml.Node) (Section, error)

	// CombineSections returns a newly allocated concrete type containing the
	// result of combining the supplied sections in order.
	CombineSections(sections ...Section) (Section, error)

	// ValidatePlan takes the complete plan as input, and allows the
	// extension to validate the plan. This can be used for cross section
	// dependency validation.
	ValidatePlan(plan *Plan) error
}

type Section interface {
	// Validate checks whether the section is valid, returning an error if not.
	Validate() error

	// IsZero reports whether the section is empty.
	IsZero() bool
}

const (
	defaultBackoffDelay  = 500 * time.Millisecond
	defaultBackoffFactor = 2.0
	defaultBackoffLimit  = 30 * time.Second

	defaultCheckPeriod    = 10 * time.Second
	defaultCheckTimeout   = 3 * time.Second
	defaultCheckThreshold = 3
)

var (
	// sectionExtensions keeps a map of registered extensions.
	sectionExtensions = map[string]SectionExtension{}

	// sectionExtensionsOrder records the order in which the extensions were registered.
	sectionExtensionsOrder = []string{}
)

// builtinSections represents all the built-in layer sections. This list is used
// for identifying built-in fields in this package. It is unit tested to match
// the YAML fields exposed in the Layer type, to catch inconsistencies.
var builtinSections = []string{"summary", "description", "services", "checks", "log-targets"}

// RegisterSectionExtension adds a plan schema extension. All registrations must be
// done before the plan library is used. The order in which extensions are
// registered determines the order in which the sections are marshalled.
// Extension sections are marshalled after the built-in sections.
func RegisterSectionExtension(field string, ext SectionExtension) {
	if slices.Contains(builtinSections, field) {
		panic(fmt.Sprintf("internal error: extension %q already used as built-in field", field))
	}
	if _, ok := sectionExtensions[field]; ok {
		panic(fmt.Sprintf("internal error: extension %q already registered", field))
	}
	sectionExtensions[field] = ext
	sectionExtensionsOrder = append(sectionExtensionsOrder, field)
}

// UnregisterSectionExtension removes a plan schema extension. This is only
// intended for use by tests during cleanup.
func UnregisterSectionExtension(field string) {
	delete(sectionExtensions, field)
	sectionExtensionsOrder = slices.DeleteFunc(sectionExtensionsOrder, func(n string) bool {
		return n == field
	})
}

type Plan struct {
	Layers     []*Layer              `yaml:"-"`
	Services   map[string]*Service   `yaml:"services,omitempty"`
	Checks     map[string]*Check     `yaml:"checks,omitempty"`
	LogTargets map[string]*LogTarget `yaml:"log-targets,omitempty"`

	Sections map[string]Section `yaml:",inline"`
}

// MarshalYAML implements an override for top level omitempty tags handling.
// This is required since Sections are based on an inlined map, for which
// omitempty and inline together is not currently supported.
func (p *Plan) MarshalYAML() (any, error) {
	// Define the content inside a structure so we can control the ordering
	// of top level sections.
	ordered := []reflect.StructField{{
		Name: "Services",
		Type: reflect.TypeOf(p.Services),
		Tag:  `yaml:"services,omitempty"`,
	}, {
		Name: "Checks",
		Type: reflect.TypeOf(p.Checks),
		Tag:  `yaml:"checks,omitempty"`,
	}, {
		Name: "LogTargets",
		Type: reflect.TypeOf(p.LogTargets),
		Tag:  `yaml:"log-targets,omitempty"`,
	}}
	for i, field := range sectionExtensionsOrder {
		section := p.Sections[field]
		ordered = append(ordered, reflect.StructField{
			Name: fmt.Sprintf("Dummy%v", i),
			Type: reflect.TypeOf(section),
			Tag:  reflect.StructTag(fmt.Sprintf("yaml:\"%s,omitempty\"", field)),
		})
	}
	typ := reflect.StructOf(ordered)
	// Assign the plan data to the structure layout we created.
	v := reflect.New(typ).Elem()
	v.Field(0).Set(reflect.ValueOf(p.Services))
	v.Field(1).Set(reflect.ValueOf(p.Checks))
	v.Field(2).Set(reflect.ValueOf(p.LogTargets))
	for i, field := range sectionExtensionsOrder {
		v.Field(3 + i).Set(reflect.ValueOf(p.Sections[field]))
	}
	plan := v.Addr().Interface()
	return plan, nil
}

// Layer represents an unmarshalled YAML layer configuration file. Layer files
// are maintained as part of the Plan, ordered by their respective order
// number. Each layer configuration also has a unique label, used for locating
// and updating a specific layer configuration.
//
// Pebble supports a two-level layer configuration directory structure. In the
// root layers directory, both layer files and layer sub-directories are
// allowed. Within a sub-directory, only layer files are allowed.
//
// Please see ReadLayersDir for more details.
type Layer struct {
	Order       int                   `yaml:"-"`
	Label       string                `yaml:"-"`
	Summary     string                `yaml:"summary,omitempty"`
	Description string                `yaml:"description,omitempty"`
	Services    map[string]*Service   `yaml:"services,omitempty"`
	Checks      map[string]*Check     `yaml:"checks,omitempty"`
	LogTargets  map[string]*LogTarget `yaml:"log-targets,omitempty"`

	Sections map[string]Section `yaml:",inline"`
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
	WorkingDir  string            `yaml:"working-dir,omitempty"`

	// Auto-restart and backoff functionality
	OnSuccess      ServiceAction            `yaml:"on-success,omitempty"`
	OnFailure      ServiceAction            `yaml:"on-failure,omitempty"`
	OnCheckFailure map[string]ServiceAction `yaml:"on-check-failure,omitempty"`
	BackoffDelay   OptionalDuration         `yaml:"backoff-delay,omitempty"`
	BackoffFactor  OptionalFloat            `yaml:"backoff-factor,omitempty"`
	BackoffLimit   OptionalDuration         `yaml:"backoff-limit,omitempty"`
	KillDelay      OptionalDuration         `yaml:"kill-delay,omitempty"`
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
		copied.UserID = copyIntPtr(s.UserID)
	}
	if s.GroupID != nil {
		copied.GroupID = copyIntPtr(s.GroupID)
	}
	if s.OnCheckFailure != nil {
		copied.OnCheckFailure = make(map[string]ServiceAction)
		for k, v := range s.OnCheckFailure {
			copied.OnCheckFailure[k] = v
		}
	}
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
		s.UserID = copyIntPtr(other.UserID)
	}
	if other.User != "" {
		s.User = other.User
	}
	if other.GroupID != nil {
		s.GroupID = copyIntPtr(other.GroupID)
	}
	if other.Group != "" {
		s.Group = other.Group
	}
	if other.WorkingDir != "" {
		s.WorkingDir = other.WorkingDir
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
func (s *Service) LogsTo(t *LogTarget) bool {
	// Iterate backwards through t.Services until we find something matching
	// s.Name.
	for i := len(t.Services) - 1; i >= 0; i-- {
		switch t.Services[i] {
		case s.Name:
			return true
		case ("-" + s.Name):
			return false
		case "all":
			return true
		case "-all":
			return false
		}
	}
	// Nothing matching the service name, so it was not specified.
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
	// Actions allowed in all contexts
	ActionUnset    ServiceAction = ""
	ActionRestart  ServiceAction = "restart"
	ActionShutdown ServiceAction = "shutdown"
	ActionIgnore   ServiceAction = "ignore"

	// Actions only allowed in specific contexts
	ActionFailureShutdown ServiceAction = "failure-shutdown"
	ActionSuccessShutdown ServiceAction = "success-shutdown"
)

// Check specifies configuration for a single health check.
type Check struct {
	// Basic details
	Name     string       `yaml:"-"`
	Override Override     `yaml:"override,omitempty"`
	Level    CheckLevel   `yaml:"level,omitempty"`
	Startup  CheckStartup `yaml:"startup,omitempty"`

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
	if other.Startup != "" {
		c.Startup = other.Startup
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

// CheckStartup defines the different startup modes for a check.
type CheckStartup string

const (
	CheckStartupUnknown  CheckStartup = ""
	CheckStartupEnabled  CheckStartup = "enabled"
	CheckStartupDisabled CheckStartup = "disabled"
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
	Command        string            `yaml:"command,omitempty"`
	ServiceContext string            `yaml:"service-context,omitempty"`
	Environment    map[string]string `yaml:"environment,omitempty"`
	UserID         *int              `yaml:"user-id,omitempty"`
	User           string            `yaml:"user,omitempty"`
	GroupID        *int              `yaml:"group-id,omitempty"`
	Group          string            `yaml:"group,omitempty"`
	WorkingDir     string            `yaml:"working-dir,omitempty"`
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
		copied.UserID = copyIntPtr(c.UserID)
	}
	if c.GroupID != nil {
		copied.GroupID = copyIntPtr(c.GroupID)
	}
	return &copied
}

// Merge merges the fields set in other into c.
func (c *ExecCheck) Merge(other *ExecCheck) {
	if other.Command != "" {
		c.Command = other.Command
	}
	if other.ServiceContext != "" {
		c.ServiceContext = other.ServiceContext
	}
	for k, v := range other.Environment {
		if c.Environment == nil {
			c.Environment = make(map[string]string)
		}
		c.Environment[k] = v
	}
	if other.UserID != nil {
		c.UserID = copyIntPtr(other.UserID)
	}
	if other.User != "" {
		c.User = other.User
	}
	if other.GroupID != nil {
		c.GroupID = copyIntPtr(other.GroupID)
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
	Name     string            `yaml:"-"`
	Type     LogTargetType     `yaml:"type"`
	Location string            `yaml:"location"`
	Services []string          `yaml:"services"`
	Override Override          `yaml:"override,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}

// LogTargetType defines the protocol to use to forward logs.
type LogTargetType string

const (
	LokiTarget     LogTargetType = "loki"
	SyslogTarget   LogTargetType = "syslog"
	UnsetLogTarget LogTargetType = ""
)

// Copy returns a deep copy of the log target configuration.
func (t *LogTarget) Copy() *LogTarget {
	copied := *t
	copied.Services = append([]string(nil), t.Services...)
	if t.Labels != nil {
		copied.Labels = make(map[string]string)
		for k, v := range t.Labels {
			copied.Labels[k] = v
		}
	}
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
	t.Services = append(t.Services, other.Services...)
	for k, v := range other.Labels {
		if t.Labels == nil {
			t.Labels = make(map[string]string)
		}
		t.Labels[k] = v
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
// Neither the individual layers nor the combined layer are validated here - the
// caller should have validated the individual layers prior to calling, and
// validate the combined output if required.
func CombineLayers(layers ...*Layer) (*Layer, error) {
	combined := &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
		Sections:   make(map[string]Section),
	}

	// Combine the same sections from each layer. Note that we do this before
	// the layers length check because we need the extension to provide us with
	// a zero value section, even if no layers are supplied (similar to the
	// allocations taking place above for the built-in types).
	for field, extension := range sectionExtensions {
		var sections []Section
		for _, layer := range layers {
			if section := layer.Sections[field]; section != nil {
				sections = append(sections, section)
			}
		}
		var err error
		combined.Sections[field], err = extension.CombineSections(sections...)
		if err != nil {
			return nil, err
		}
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

	// Set defaults where required.
	for _, service := range combined.Services {
		if !service.BackoffDelay.IsSet {
			service.BackoffDelay.Value = defaultBackoffDelay
		}
		if !service.BackoffFactor.IsSet {
			service.BackoffFactor.Value = defaultBackoffFactor
		}
		if !service.BackoffLimit.IsSet {
			service.BackoffLimit.Value = defaultBackoffLimit
		}
	}

	for _, check := range combined.Checks {
		if !check.Period.IsSet {
			check.Period.Value = defaultCheckPeriod
		}
		if !check.Timeout.IsSet {
			check.Timeout.Value = defaultCheckTimeout
		}
		if check.Timeout.Value > check.Period.Value {
			// The effective timeout will be the period, so make that clear.
			// `.IsSet` remains false so that the capped value does not appear
			// in the combined plan output - and it's not *user* set - the
			// effective default timeout is the minimum of (check.Period.Value,
			// default timeout).
			check.Timeout.Value = check.Period.Value
		}
		if check.Threshold == 0 {
			// Default number of failures in a row before check triggers
			// action, default is >1 to avoid flapping due to glitches. For
			// what it's worth, Kubernetes probes uses a default of 3 too.
			check.Threshold = defaultCheckThreshold
		}
	}

	return combined, nil
}

// Validate checks that the layer is valid. It returns nil if all the checks pass, or
// an error if there are validation errors.
// See also Plan.Validate, which does additional checks based on the combined
// layers.
func (layer *Layer) Validate() error {
	if strings.HasPrefix(layer.Label, "pebble-") {
		return &FormatError{
			Message: `cannot use reserved label prefix "pebble-"`,
		}
	}

	for name, service := range layer.Services {
		if name == "" {
			return &FormatError{
				Message: "cannot use empty string as service name",
			}
		}
		if name == "pebble" {
			// Disallow service name "pebble" to avoid ambiguity (for example,
			// in log output).
			return &FormatError{
				Message: fmt.Sprintf("cannot use reserved service name %q", name),
			}
		}
		// Deprecated service names
		if name == "all" || name == "default" || name == "none" {
			logger.Noticef("Using keyword %q as a service name is deprecated", name)
		}
		if strings.HasPrefix(name, "-") {
			return &FormatError{
				Message: fmt.Sprintf(`cannot use service name %q: starting with "-" not allowed`, name),
			}
		}
		if service == nil {
			return &FormatError{
				Message: fmt.Sprintf("service object cannot be null for service %q", name),
			}
		}
		_, _, err := service.ParseCommand()
		if err != nil {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q command invalid: %v", name, err),
			}
		}
		if !validServiceAction(service.OnSuccess, ActionFailureShutdown) {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q on-success action %q invalid", name, service.OnSuccess),
			}
		}
		if !validServiceAction(service.OnFailure, ActionSuccessShutdown) {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q on-failure action %q invalid", name, service.OnFailure),
			}
		}
		for _, action := range service.OnCheckFailure {
			if !validServiceAction(action, ActionSuccessShutdown) {
				return &FormatError{
					Message: fmt.Sprintf("plan service %q on-check-failure action %q invalid", name, action),
				}
			}
		}
		if service.BackoffFactor.IsSet && service.BackoffFactor.Value < 1 {
			return &FormatError{
				Message: fmt.Sprintf("plan service %q backoff-factor must be 1.0 or greater, not %g", name, service.BackoffFactor.Value),
			}
		}
	}

	for name, check := range layer.Checks {
		if name == "" {
			return &FormatError{
				Message: "cannot use empty string as check name",
			}
		}
		if check == nil {
			return &FormatError{
				Message: fmt.Sprintf("check object cannot be null for check %q", name),
			}
		}
		if name == "" {
			return &FormatError{
				Message: "cannot use empty string as log target name",
			}
		}
		if check.Level != UnsetLevel && check.Level != AliveLevel && check.Level != ReadyLevel {
			return &FormatError{
				Message: fmt.Sprintf(`plan check %q level must be "alive" or "ready"`, name),
			}
		}
		if check.Startup != CheckStartupUnknown && check.Startup != CheckStartupEnabled && check.Startup != CheckStartupDisabled {
			return &FormatError{
				Message: fmt.Sprintf(`plan check %q startup must be "enabled" or "disabled"`, name),
			}
		}
		if check.Period.IsSet && check.Period.Value == 0 {
			return &FormatError{
				Message: fmt.Sprintf("plan check %q period must not be zero", name),
			}
		}
		if check.Timeout.IsSet && check.Timeout.Value == 0 {
			return &FormatError{
				Message: fmt.Sprintf("plan check %q timeout must not be zero", name),
			}
		}

		if check.Exec != nil {
			_, err := shlex.Split(check.Exec.Command)
			if err != nil {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q command invalid: %v", name, err),
				}
			}
			_, _, err = osutil.NormalizeUidGid(check.Exec.UserID, check.Exec.GroupID, check.Exec.User, check.Exec.Group)
			if err != nil {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q has invalid user/group: %v", name, err),
				}
			}
		}
	}

	for name, target := range layer.LogTargets {
		if target == nil {
			return &FormatError{
				Message: fmt.Sprintf("log target object cannot be null for log target %q", name),
			}
		}
		for labelName := range target.Labels {
			// 'pebble_*' labels are reserved
			if strings.HasPrefix(labelName, "pebble_") {
				return &FormatError{
					Message: fmt.Sprintf(`log target %q: label %q uses reserved prefix "pebble_"`, name, labelName),
				}
			}
		}
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			// will be checked when the layers are combined
		default:
			return &FormatError{
				Message: fmt.Sprintf(`log target %q has unsupported type %q, must be %q or %q`,
					name, target.Type, LokiTarget, SyslogTarget),
			}
		}
	}

	for _, section := range layer.Sections {
		err := section.Validate()
		if err != nil {
			return err
		}
	}

	return nil
}

// Validate checks that the combined layers form a valid plan. See also
// Layer.Validate, which checks that the individual layers are valid.
func (p *Plan) Validate() error {
	for name, service := range p.Services {
		if service.Command == "" {
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "command" for service %q`, name),
			}
		}
	}

	for name, check := range p.Checks {
		numTypes := 0
		if check.HTTP != nil {
			if check.HTTP.URL == "" {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "url" for http check %q`, name),
				}
			}
			numTypes++
		}
		if check.TCP != nil {
			if check.TCP.Port == 0 {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "port" for tcp check %q`, name),
				}
			}
			numTypes++
		}
		if check.Exec != nil {
			if check.Exec.Command == "" {
				return &FormatError{
					Message: fmt.Sprintf(`plan must set "command" for exec check %q`, name),
				}
			}
			_, contextExists := p.Services[check.Exec.ServiceContext]
			if check.Exec.ServiceContext != "" && !contextExists {
				return &FormatError{
					Message: fmt.Sprintf("plan check %q service context specifies non-existent service %q",
						name, check.Exec.ServiceContext),
				}
			}
			numTypes++
		}
		if numTypes != 1 {
			return &FormatError{
				Message: fmt.Sprintf(`plan must specify one of "http", "tcp", or "exec" for check %q`, name),
			}
		}
	}

	for name, target := range p.LogTargets {
		switch target.Type {
		case LokiTarget, SyslogTarget:
			// valid, continue
		case UnsetLogTarget:
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "type" (%q or %q) for log target %q`,
					LokiTarget, SyslogTarget, name),
			}
		}

		// Validate service names specified in log target.
		for _, serviceName := range target.Services {
			serviceName = strings.TrimPrefix(serviceName, "-")
			if serviceName == "all" {
				continue
			}
			if _, ok := p.Services[serviceName]; ok {
				continue
			}
			return &FormatError{
				Message: fmt.Sprintf(`log target %q specifies unknown service %q`,
					target.Name, serviceName),
			}
		}

		if target.Location == "" {
			return &FormatError{
				Message: fmt.Sprintf(`plan must define "location" for log target %q`, name),
			}
		}
	}

	// Ensure combined layers don't have cycles.
	err := p.checkCycles()
	if err != nil {
		return err
	}

	// Each section extension must validate the combined plan.
	for _, extension := range sectionExtensions {
		err = extension.ValidatePlan(p)
		if err != nil {
			return err
		}
	}

	return nil
}

// StartOrder returns the required services that must be started for the named
// services to be properly started, in the order that they must be started.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StartOrder(names []string) ([][]string, error) {
	orderedNames, err := order(p.Services, names, false)
	if err != nil {
		return nil, err
	}

	return createLanes(orderedNames, p.Services)
}

// StopOrder returns the required services that must be stopped for the named
// services to be properly stopped, in the order that they must be stopped.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (p *Plan) StopOrder(names []string) ([][]string, error) {
	orderedNames, err := order(p.Services, names, true)
	if err != nil {
		return nil, err
	}

	return createLanes(orderedNames, p.Services)
}

func getOrCreateLane(currentLane int, service *Service, serviceLaneMapping map[string]int) int {
	// if the service has been mapped to a lane
	if lane, ok := serviceLaneMapping[service.Name]; ok {
		mapServiceToLane(service, lane, serviceLaneMapping)
		return lane
	}

	// if any dependency has been mapped to a lane
	for _, dependency := range service.Requires {
		if lane, ok := serviceLaneMapping[dependency]; ok {
			mapServiceToLane(service, lane, serviceLaneMapping)
			return lane
		}
	}

	// neither the service itself nor any of its dependencies is mapped to an existing lane
	lane := currentLane + 1
	mapServiceToLane(service, lane, serviceLaneMapping)
	return lane
}

func mapServiceToLane(service *Service, lane int, serviceLaneMapping map[string]int) {
	serviceLaneMapping[service.Name] = lane

	// map the service's dependencies to the same lane
	for _, dependency := range service.Requires {
		serviceLaneMapping[dependency] = lane
	}
}

func createLanes(names []string, services map[string]*Service) ([][]string, error) {
	serviceLaneMapping := make(map[string]int)

	// Map all services into lanes.
	lane := -1
	maxLane := 0
	for _, name := range names {
		service, ok := services[name]
		if !ok {
			return nil, &FormatError{
				Message: fmt.Sprintf("service %q does not exist", name),
			}
		}

		lane = getOrCreateLane(lane, service, serviceLaneMapping)
		if lane > maxLane {
			maxLane = lane
		}
	}

	// Create lanes
	lanes := make([][]string, maxLane+1)
	for _, service := range names {
		lane := serviceLaneMapping[service]
		lanes[lane] = append(lanes[lane], service)
	}
	return lanes, nil
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

func (p *Plan) checkCycles() error {
	var names []string
	for name := range p.Services {
		names = append(names, name)
	}
	_, err := order(p.Services, names, false)
	return err
}

func ParseLayer(order int, label string, data []byte) (*Layer, error) {
	layer := &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
		Sections:   make(map[string]Section),
	}

	// The following manual approach is required because:
	//
	// 1. Extended sections are YAML inlined, and also do not have a
	// concrete type at this level, we cannot simply unmarshal the layer
	// in one step.
	//
	// 2. We honor KnownFields = true behaviour for non extended schema
	// sections, and at the top field level, which includes Section field
	// names.
	builtins := map[string]any{
		"summary":     &layer.Summary,
		"description": &layer.Description,
		"services":    &layer.Services,
		"checks":      &layer.Checks,
		"log-targets": &layer.LogTargets,
	}

	sections := make(map[string]yaml.Node)
	// Deliberately pre-allocate at least an empty yaml.Node for every
	// extension section. Extension sections that have unmarshalled
	// will update the respective node, while non-existing sections
	// will at least have an empty node. This means we can consistently
	// let the extension allocate and decode the yaml node for all sections,
	// and in the case where it is zero, we get an empty backing type instance.
	for field := range sectionExtensions {
		sections[field] = yaml.Node{}
	}
	err := yaml.Unmarshal(data, &sections)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}

	for field, section := range sections {
		if slices.Contains(builtinSections, field) {
			// The following issue prevents us from using the yaml.Node decoder
			// with KnownFields = true behaviour. Once one of the proposals get
			// merged, we can remove the intermediate Marshal step.
			// https://github.com/go-yaml/yaml/issues/460
			data, err := yaml.Marshal(&section)
			if err != nil {
				return nil, fmt.Errorf("internal error: cannot marshal %v section: %w", field, err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			if err = dec.Decode(builtins[field]); err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("cannot parse layer %q section %q: %v", label, field, err),
				}
			}
		} else {
			extension, ok := sectionExtensions[field]
			if !ok {
				// At the top level we do not ignore keys we do not understand.
				// This preserves the current Pebble behaviour of decoding with
				// KnownFields = true.
				return nil, &FormatError{
					Message: fmt.Sprintf("cannot parse layer %q: unknown section %q", label, field),
				}
			}

			// Section unmarshal rules are defined by the extension itself.
			layer.Sections[field], err = extension.ParseSection(section)
			if err != nil {
				return nil, err
			}
		}
	}

	layer.Order = order
	layer.Label = label

	for name, service := range layer.Services {
		// If service is nil, then the validation below will reject the layer,
		// but we want the name set so that we can use easily use it in error
		// messages during validation.
		if service != nil {
			service.Name = name
		}
	}
	for name, check := range layer.Checks {
		if check != nil {
			check.Name = name
		}
	}
	for name, target := range layer.LogTargets {
		if target != nil {
			target.Name = name
		}
	}

	err = layer.Validate()
	if err != nil {
		return nil, err
	}

	return layer, err
}

func validServiceAction(action ServiceAction, additionalValid ...ServiceAction) bool {
	for _, v := range additionalValid {
		if action == v {
			return true
		}
	}
	switch action {
	case ActionUnset, ActionRestart, ActionShutdown, ActionIgnore:
		return true
	default:
		return false
	}
}

// ReadLayersDir loads the YAML layer files from the first two directory
// levels starting at layersDir in the order as specified by the order
// directory and file order prefixes. The directory and file suffixes
// are dropped in the returned labels.
//
//	| File (inside layersDir)    | Order           | Label   |
//	| -------------------------- | --------------- | ------- |
//	| 001-foo.yaml               | 001-000 => 1000 | foo     |
//	| 002-bar.d/001-aaa.yaml     | 002-001 => 2001 | bar/aaa |
//	| 002-bar.d/002-bbb.yaml     | 002-002 => 2002 | bar/bbb |
//	| 003-baz.yaml               | 003-000 => 3000 | baz     |
func ReadLayersDir(layersDir string) ([]*Layer, error) {
	var layers []*Layer

	// Read the first-level directory
	l1Entries, err := configLayerEntries(layersDir, true)
	if err != nil {
		return nil, err
	}

	for _, l1Entry := range l1Entries {
		l1Path := filepath.Join(layersDir, l1Entry.name)

		// Let's check if the path (including a symlink) is a directory.
		info, err := os.Stat(l1Path)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			// Read the second-level directory
			l2Entries, err := configLayerEntries(l1Path, false)
			if err != nil {
				return nil, err
			}

			// Add the config files from the second level
			for _, l2Entry := range l2Entries {
				layer, err := loadConfigLayer(layersDir, l1Entry, l2Entry)
				if err != nil {
					return nil, err
				}
				layers = append(layers, layer)
			}
		} else {
			// Add the config files from the first level
			layer, err := loadConfigLayer(layersDir, l1Entry, nil)
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)
		}
	}

	return layers, nil
}

// loadConfigLayer loads a layer configuration file and returns a Layer
// on success. The layer configuration is typically in the Pebble layers
// root directory, in which case l2Entry must be nil. If the file is
// inside a sub-directory, l1Entry must supply information on the directory,
// while l2Entry information on the file name itself.
func loadConfigLayer(layersDir string, l1Entry *configEntry, l2Entry *configEntry) (*Layer, error) {
	// Resolve the order and label, which may include additional
	// information from an optional sub-directory prefix.
	label := l1Entry.label
	order := 1000 * l1Entry.order
	path := filepath.Join(layersDir, l1Entry.name)
	if l2Entry != nil {
		// Config layer is inside a sub-directory.
		label = label + "/" + l2Entry.label
		order = order + l2Entry.order
		path = filepath.Join(path, l2Entry.name)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// Errors from package os generally include the path.
		return nil, fmt.Errorf("cannot read layer file: %v", err)
	}

	layer, err := ParseLayer(order, label, data)
	if err != nil {
		return nil, err
	}

	return layer, nil
}

// configEntryRegexp matches either a valid config layer YAML file name or a
// valid config layer directory. Match[1] is the 3-digit order and match[2]
// is the label.
var configEntryRegexp = regexp.MustCompile(`^([0-9]{3})-([a-z](?:-?[a-z0-9]){2,})(.yaml|.d)$`)

type configEntry struct {
	name  string
	order int
	label string
}

// configLayerEntries reads a directory containing config layer files or
// sub-directories and validates that the naming is valid. If dirOK is
// set to false it will not permit sub-directories within the supplied
// configDir path. The returned string slice is ordered alphanumerically
// (so names are automatically ordered by their 'order').
func configLayerEntries(configDir string, dirOK bool) (configs []*configEntry, err error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read layers directory: %v", err)
	}

	orders := make(map[int]string)
	labels := make(map[string]int)
	for _, entry := range entries {
		// Let's not fail to start the system up if some unrelated file ended
		// up in the layers directory by accident.
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".d") {
			continue
		}

		// Let's check if the path (including a symlink) is a directory.
		info, err := os.Stat(filepath.Join(configDir, entry.Name()))
		if err != nil {
			return nil, err
		}

		// Let's ensure the file or sub-directory name is valid.
		match := configEntryRegexp.FindStringSubmatch(entry.Name())
		if match == nil {
			if info.IsDir() {
				return nil, fmt.Errorf("invalid layer sub-directory name: %q (must look like \"123-some-label.d\")", entry.Name())
			} else {
				return nil, fmt.Errorf("invalid layer filename: %q (must look like \"123-some-label.yaml\")", entry.Name())
			}
		}

		// Only the root layers directory support sub-directories.
		if info.IsDir() && !dirOK {
			return nil, fmt.Errorf("cannot have a layers sub-directory at this level")
		}

		// Extract the order and label from the match.
		label := match[2]
		order, err := strconv.Atoi(match[1])
		if err != nil {
			panic(fmt.Sprintf("internal error: filename regexp is wrong: %v", err))
		}

		// Let's make sure no duplicate orders or labels appear.
		oldLabel, dupOrder := orders[order]
		oldOrder, dupLabel := labels[label]
		if dupOrder {
			oldOrder = order
		} else if dupLabel {
			oldLabel = label
		}
		if dupOrder || dupLabel {
			return nil, fmt.Errorf("invalid layer filename: %q not unique (have \"%03d-%s.yaml\" already)", entry.Name(), oldOrder, oldLabel)
		}
		orders[order] = label
		labels[label] = order

		// All is good for this entry.
		configs = append(configs, &configEntry{
			name:  entry.Name(),
			order: order,
			label: label,
		})
	}

	return configs, nil
}

// ReadDir reads the configuration layers from layersDir,
// and returns the resulting Plan. If layersDir doesn't
// exist, it returns a valid Plan with no layers.
func ReadDir(layersDir string) (*Plan, error) {
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
		Sections:   combined.Sections,
	}
	err = plan.Validate()
	if err != nil {
		return nil, err
	}
	return plan, err
}

// MergeServiceContext merges the overrides on top of the service context
// specified by serviceName, returning a new ContextOptions value. If
// serviceName is "" (context not specified), return overrides directly.
func MergeServiceContext(p *Plan, serviceName string, overrides ContextOptions) (ContextOptions, error) {
	if serviceName == "" {
		return overrides, nil
	}
	var service *Service
	for _, s := range p.Services {
		if s.Name == serviceName {
			service = s
			break
		}
	}
	if service == nil {
		return ContextOptions{}, fmt.Errorf("context service %q not found", serviceName)
	}

	// Start with the config values from the context service.
	merged := ContextOptions{
		Environment: make(map[string]string),
	}
	for k, v := range service.Environment {
		merged.Environment[k] = v
	}
	if service.UserID != nil {
		merged.UserID = copyIntPtr(service.UserID)
	}
	merged.User = service.User
	if service.GroupID != nil {
		merged.GroupID = copyIntPtr(service.GroupID)
	}
	merged.Group = service.Group
	merged.WorkingDir = service.WorkingDir

	// Merge in fields from the overrides, if set.
	for k, v := range overrides.Environment {
		merged.Environment[k] = v
	}
	if overrides.UserID != nil {
		merged.UserID = copyIntPtr(overrides.UserID)
	}
	if overrides.User != "" {
		merged.User = overrides.User
	}
	if overrides.GroupID != nil {
		merged.GroupID = copyIntPtr(overrides.GroupID)
	}
	if overrides.Group != "" {
		merged.Group = overrides.Group
	}
	if overrides.WorkingDir != "" {
		merged.WorkingDir = overrides.WorkingDir
	}

	return merged, nil
}

// ContextOptions holds service context config fields.
type ContextOptions struct {
	Environment map[string]string
	UserID      *int
	User        string
	GroupID     *int
	Group       string
	WorkingDir  string
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}
