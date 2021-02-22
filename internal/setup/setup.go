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

package setup

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Setup struct {
	Layers []*Layer
}

func (s *Setup) AddLayer(layer *Layer) {
	s.Layers = append(s.Layers, layer)
}

type Layer struct {
	Order       int    `yaml:"-"`
	Label       string `yaml:"-"`
	Summary     string `yaml:"summary,omitempty"`
	Description string `yaml:"description,omitempty"`
	Services    map[string]*Service
}

func (l *Layer) AsYAML() ([]byte, error) {
	return yaml.Marshal(l)
}

type Service struct {
	Name        string           `yaml:"-"`
	Summary     string           `yaml:"summary,omitempty"`
	Description string           `yaml:"description,omitempty"`
	Default     ServiceAction    `yaml:"default,omitempty"`
	Override    ServiceOverride  `yaml:"override,omitempty"`
	Command     string           `yaml:"command,omitempty"`
	After       []string         `yaml:"after,omitempty"`
	Before      []string         `yaml:"before,omitempty"`
	Requires    []string         `yaml:"requires,omitempty"`
	Environment []StringVariable `yaml:"environment,omitempty"`
}

type ServiceAction string

const (
	UnknownAction ServiceAction = ""
	StartAction   ServiceAction = "start"
	StopAction    ServiceAction = "stop"
)

type ServiceOverride string

const (
	UnknownOverride ServiceOverride = ""
	MergeOverride   ServiceOverride = "merge"
	ReplaceOverride ServiceOverride = "replace"
)

type StringVariable struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

func (sv *StringVariable) UnmarshalYAML(node *yaml.Node) error {
	if node.ShortTag() != "!!map" || len(node.Content) != 2 {
		return fmt.Errorf("environment must be a list of single-item maps (- name: value)")
	}
	var name, value string
	err := node.Content[0].Decode(&name)
	if err == nil {
		err = node.Content[1].Decode(&value)
	}
	if err != nil {
		return fmt.Errorf("cannot decode environment variable: %v", err)
	}
	sv.Name = name
	sv.Value = value
	return nil
}

func (s *Setup) Flatten() (*Layer, error) {
	var flat Layer
	flat.Services = make(map[string]*Service)
	if len(s.Layers) == 0 {
		return &flat, nil
	}
	last := s.Layers[len(s.Layers)-1]
	flat.Summary = last.Summary
	flat.Description = last.Description
	for _, layer := range s.Layers {
		for name, service := range layer.Services {
			switch service.Override {
			case MergeOverride:
				if old, ok := flat.Services[name]; ok {
					if service.Summary != "" {
						old.Summary = service.Summary
					}
					if service.Description != "" {
						old.Description = service.Description
					}
					if service.Default != UnknownAction {
						old.Default = service.Default
					}
					if service.Command != "" {
						old.Command = service.Command
					}
					old.Before = append(old.Before, service.Before...)
					old.After = append(old.After, service.After...)
					old.Environment = append(old.Environment, service.Environment...)
					break
				}
				fallthrough
			case ReplaceOverride:
				copy := *service
				flat.Services[name] = &copy
			case UnknownOverride:
				return nil, fmt.Errorf("layer %q must define 'override' for service %q",
					layer.Label, service.Name)
			default:
				return nil, fmt.Errorf("layer %q has invalid 'override' value on service %q: %q",
					layer.Label, service.Name, service.Override)
			}
		}
	}
	return &flat, nil
}

// StartOrder returns the required services that must be started for the named
// services to be properly started, in the order that they must be started.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (l *Layer) StartOrder(names []string) ([]string, error) {
	return l.order(names, false)
}

// StopOrder returns the required services that must be stopped for the named
// services to be properly stopped, in the order that they must be stopped.
// An error is returned when a provided service name does not exist, or there
// is an order cycle involving the provided service or its dependencies.
func (l *Layer) StopOrder(names []string) ([]string, error) {
	return l.order(names, true)
}

func (l *Layer) order(names []string, stop bool) ([]string, error) {

	// For stop, create a list of reversed dependencies.
	predecessors := map[string][]string(nil)
	if stop {
		predecessors = make(map[string][]string)
		for name, service := range l.Services {
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
			service, ok := l.Services[name]
			if !ok {
				return nil, fmt.Errorf("service %q does not exist", name)
			}
			pending = append(pending, service.Requires...)
		}
	}

	// Create a list of successors involving those services only.
	for name := range successors {
		service, ok := l.Services[name]
		if !ok {
			return nil, fmt.Errorf("service %q does not exist", name)
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
			return nil, fmt.Errorf("services in before/after loop: %s", strings.Join(names, ", "))
		}
		order = append(order, names[0])
	}
	return order, nil
}

func (l *Layer) CheckCycles() error {
	var names []string
	for name := range l.Services {
		names = append(names, name)
	}
	_, err := l.StartOrder(names)
	return err
}

func ParseLayer(order int, label string, data []byte) (*Layer, error) {
	var layer Layer
	dec := yaml.NewDecoder(bytes.NewBuffer(data))
	dec.KnownFields(true)
	err := dec.Decode(&layer)
	if err != nil {
		return nil, fmt.Errorf("cannot parse layer %q: %v", label, err)
	}
	layer.Order = order
	layer.Label = label
	for name, service := range layer.Services {
		service.Name = name
	}
	err = layer.CheckCycles()
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
			panic("internal error: filename regexp is wrong")
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

func ReadDir(dir string) (*Setup, error) {
	layers, err := ReadLayersDir(filepath.Join(dir, "layers"))
	if err != nil {
		return nil, err
	}
	setup := &Setup{
		Layers: layers,
	}
	return setup, nil
}
