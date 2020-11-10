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
	Key         string
	Summary     string
	Description string
	Services    map[string]*Service
}

type Service struct {
	Name        string
	Summary     string
	Description string
	State       ServiceState
	Override    ServiceOverride
	Command     string
	Before      []string
	After       []string
	Environment []StringVariable
}

type ServiceState string

const (
	UnknownState  ServiceState = ""
	EnabledState  ServiceState = "enabled"
	DisabledState ServiceState = "disabled"
)

type ServiceOverride string

const (
	UnknownOverride ServiceOverride = ""
	MergeOverride   ServiceOverride = "merge"
	ReplaceOverride ServiceOverride = "replace"
)

type StringVariable struct {
	Name, Value string
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
					if service.State != UnknownState {
						old.State = service.State
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
				return nil, fmt.Errorf("layer %s must define 'override' for service %q",
					layer.Key, service.Name)
			default:
				return nil, fmt.Errorf("layer %s has invalid 'override' value on service %q: %q",
					layer.Key, service.Name, service.Override)
			}
		}
	}
	return &flat, nil
}

func (l *Layer) ServiceOrder() ([]string, error) {
	successors := make(map[string][]string)
	for name, service := range l.Services {
		successors[name] = append(successors[name], service.After...)
		for _, before := range service.Before {
			successors[before] = append(successors[before], name)
		}
	}
	var order []string
	for _, names := range tarjanSort(successors) {
		if len(names) > 1 {
			return nil, fmt.Errorf("services in before/after loop: %s", strings.Join(names, ", "))
		}
		order = append(order, names[0])
	}
	return order, nil
}

func ParseLayer(key string, data []byte) (*Layer, error) {
	var layer Layer
	dec := yaml.NewDecoder(bytes.NewBuffer(data))
	dec.KnownFields(true)
	err := dec.Decode(&layer)
	if err != nil {
		return nil, fmt.Errorf("cannot parse layer %s: %v", key, err)
	}
	layer.Key = key
	for name, service := range layer.Services {
		service.Name = name
	}
	_, err = layer.ServiceOrder()
	if err != nil {
		return nil, err
	}
	return &layer, err
}

func ParseLayersDir(dirname string) ([]*Layer, error) {
	finfos, err := ioutil.ReadDir(dirname)
	if err != nil {
		// Errors from package os generally include the path.
		return nil, fmt.Errorf("cannot read layers directory: %v", err)
	}

	// Documentation says ReadDir result is already sorted by name.
	// This is fundamental here so if reading changes make sure the
	// sorting is preserved.
	var layers []*Layer
	for _, finfo := range finfos {
		if finfo.IsDir() {
			continue
		}
		// TODO Consider enforcing permissions and ownership here to
		//      avoid mistakes that could lead to hacks.
		data, err := ioutil.ReadFile(filepath.Join(dirname, finfo.Name()))
		if err != nil {
			// Errors from package os generally include the path.
			return nil, fmt.Errorf("cannot read layer file: %v", err)
		}
		layer, err := ParseLayer(finfo.Name(), data)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}
