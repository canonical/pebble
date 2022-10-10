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
	"reflect"
)

type Plan struct {
	Layers   []*Layer            `yaml:"-"`
	Services map[string]*Service `yaml:"services,omitempty"`
	Checks   map[string]*Check   `yaml:"checks,omitempty"`
}

type Layer struct {
	Order       int                 `yaml:"-"`
	Label       string              `yaml:"-"`
	Summary     string              `yaml:"summary,omitempty"`
	Description string              `yaml:"description,omitempty"`
	Services    map[string]*Service `yaml:"services,omitempty"`
	Checks      map[string]*Check   `yaml:"checks,omitempty"`
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
	return &copied
}

// Merge merges the fields set in other into c.
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
}

// Equal returns true when the two services are equal in value.
func (s *Service) Equal(other *Service) bool {
	if s == other {
		return true
	}
	return reflect.DeepEqual(s, other)
}

type ServiceStartup string

const (
	StartupUnknown  ServiceStartup = ""
	StartupEnabled  ServiceStartup = "enabled"
	StartupDisabled ServiceStartup = "disabled"
)

// Override specifies the layer override mechanism (for services and checks).
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
