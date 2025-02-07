// Copyright (c) 2025 Canonical Ltd
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

package servstate

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/canonical/pebble/internals/plan"
	yaml "gopkg.in/yaml.v3"
)

var _ plan.SectionExtension = (*WorkloadsSectionExtension)(nil)

type WorkloadsSectionExtension struct{}

func (ext *WorkloadsSectionExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	ws := &WorkloadsSection{}
	for _, section := range sections {
		layer, ok := section.(*WorkloadsSection)
		if !ok {
			return nil, fmt.Errorf("internal error: invalid section type %T", layer)
		}
		if err := ws.combine(layer); err != nil {
			return nil, err
		}
	}
	return ws, nil
}

func (ext *WorkloadsSectionExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	ws := &WorkloadsSection{}
	// The following issue prevents us from using the yaml.Node decoder
	// with KnownFields = true behavior. Once one of the proposals get
	// merged, we can remove the intermediate Marshal step.
	if len(data.Content) != 0 {
		yml, err := yaml.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf(`internal error: cannot marshal "workloads" section: %w`, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(yml))
		dec.KnownFields(true)
		if err = dec.Decode(ws); err != nil {
			return nil, &plan.FormatError{
				Message: fmt.Sprintf(`cannot parse the "workloads" section: %v`, err),
			}
		}
	}
	for name, workload := range ws.Entries {
		if workload != nil {
			workload.Name = name
		}
	}
	return ws, nil
}

func (ext *WorkloadsSectionExtension) ValidatePlan(p *plan.Plan) error {
	ws, ok := p.Sections[WorkloadsField].(*WorkloadsSection)
	if !ok {
		return fmt.Errorf("internal error: invalid section type %T", ws)
	}
	for name, service := range p.Services {
		_, ok := ws.Entries[service.Workload]
		if service.Workload != "" && !ok {
			return &plan.FormatError{
				Message: fmt.Sprintf(`plan service %q cannot run in unknown workload %q`, name, service.Workload),
			}
		}
	}
	return nil
}

const WorkloadsField = "workloads"

var _ plan.Section = (*WorkloadsSection)(nil)

type WorkloadsSection struct {
	Entries map[string]*Workload `yaml:",inline"`
}

func (ws *WorkloadsSection) IsZero() bool {
	return len(ws.Entries) == 0
}

func (ws *WorkloadsSection) Validate() error {
	for name, workload := range ws.Entries {
		if workload == nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q has a null value", name),
			}
		}
		if err := workload.validate(); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q %v", name, err),
			}
		}
	}
	return nil
}

func (ws *WorkloadsSection) combine(other *WorkloadsSection) error {
	if len(other.Entries) != 0 && ws.Entries == nil {
		ws.Entries = make(map[string]*Workload)
	}
	for name, workload := range other.Entries {
		switch workload.Override {
		case plan.MergeOverride:
			if current, ok := ws.Entries[name]; ok {
				copied := current.copy()
				copied.merge(workload)
				ws.Entries[name] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			ws.Entries[name] = workload.copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q must define an "override" policy`, name),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q has an invalid "override" value: %q`, name, workload.Override),
			}
		}
	}
	return nil
}

type Workload struct {
	// Basic details
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`

	// Options for command execution
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`
}

func (w *Workload) validate() error {
	if w.Name == "" {
		return errors.New("cannot have an empty name")
	}
	// Value of Override is checked in the (*WorkloadSection).combine() method
	return nil
}

func (w *Workload) copy() *Workload {
	copied := *w
	if w.Environment != nil {
		copied.Environment = make(map[string]string, len(w.Environment))
		for k, v := range w.Environment {
			copied.Environment[k] = v
		}
	}
	if w.UserID != nil {
		copied.UserID = copyIntPtr(w.UserID)
	}
	if w.GroupID != nil {
		copied.GroupID = copyIntPtr(w.GroupID)
	}
	return &copied
}

func (w *Workload) merge(other *Workload) {
	if len(other.Environment) != 0 && w.Environment == nil {
		w.Environment = make(map[string]string)
	}
	for k, v := range other.Environment {
		w.Environment[k] = v
	}
	if other.UserID != nil {
		w.UserID = copyIntPtr(other.UserID)
	}
	if other.User != "" {
		w.User = other.User
	}
	if other.GroupID != nil {
		w.GroupID = copyIntPtr(other.GroupID)
	}
	if other.Group != "" {
		w.Group = other.Group
	}
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}
