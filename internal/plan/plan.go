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

	"gopkg.in/yaml.v3"
)

type Plan struct {
	Layers   []*Layer            `yaml:"-"`
	Services map[string]*Service `yaml:"services,omitempty"`
}

type Layer struct {
	Order       int                 `yaml:"-"`
	Label       string              `yaml:"-"`
	Summary     string              `yaml:"summary,omitempty"`
	Description string              `yaml:"description,omitempty"`
	Services    map[string]*Service `yaml:"services,omitempty"`
}

type Service struct {
	Name        string            `yaml:"-"`
	Summary     string            `yaml:"summary,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Startup     ServiceStartup    `yaml:"startup,omitempty"`
	Override    ServiceOverride   `yaml:"override,omitempty"`
	Command     string            `yaml:"command,omitempty"`
	After       []string          `yaml:"after,omitempty"`
	Before      []string          `yaml:"before,omitempty"`
	Requires    []string          `yaml:"requires,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`
}

// Copy returns a deep copy of the service.
func (s *Service) Copy() *Service {
	copy := *s
	copy.After = append([]string(nil), s.After...)
	copy.Before = append([]string(nil), s.Before...)
	copy.Requires = append([]string(nil), s.Requires...)
	if s.Environment != nil {
		copy.Environment = make(map[string]string)
		for k, v := range s.Environment {
			copy.Environment[k] = v
		}
	}
	if s.UserID != nil {
		userID := *s.UserID
		copy.UserID = &userID
	}
	if s.GroupID != nil {
		groupID := *s.GroupID
		copy.GroupID = &groupID
	}
	return &copy
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

type ServiceOverride string

const (
	UnknownOverride ServiceOverride = ""
	MergeOverride   ServiceOverride = "merge"
	ReplaceOverride ServiceOverride = "replace"
)

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
		Services: make(map[string]*Service),
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
					copy := old.Copy()
					if service.Summary != "" {
						copy.Summary = service.Summary
					}
					if service.Description != "" {
						copy.Description = service.Description
					}
					if service.Startup != StartupUnknown {
						copy.Startup = service.Startup
					}
					if service.Command != "" {
						copy.Command = service.Command
					}
					if service.UserID != nil {
						copy.UserID = service.UserID
					}
					if service.User != "" {
						copy.User = service.User
					}
					if service.GroupID != nil {
						copy.GroupID = service.GroupID
					}
					if service.Group != "" {
						copy.Group = service.Group
					}
					copy.Before = append(copy.Before, service.Before...)
					copy.After = append(copy.After, service.After...)
					for k, v := range service.Environment {
						copy.Environment[k] = v
					}
					combined.Services[name] = copy
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
	}

	// Ensure fields in combined layers validate correctly.
	for name, service := range combined.Services {
		if service.Command == "" {
			return nil, &FormatError{
				Message: fmt.Sprintf(`plan must define "command" for service %q`, name),
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
	var layer Layer
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
	err = layer.checkCycles()
	if err != nil {
		return nil, err
	}
	return &layer, err
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
		Layers:   layers,
		Services: combined.Services,
	}
	return plan, err
}
