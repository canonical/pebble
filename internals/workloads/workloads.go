// Copyright (c) 2025 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a Copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workloads

import (
	"errors"
	"fmt"
	"maps"
	"reflect"

	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/plan"
)

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
	copied.Environment = maps.Clone(w.Environment)
	copied.UserID = copyPtr(w.UserID)
	copied.GroupID = copyPtr(w.GroupID)
	return &copied
}

func (w *Workload) merge(other *Workload) {
	if len(other.Environment) > 0 {
		w.Environment = makeMapIfNil(w.Environment)
		maps.Copy(w.Environment, other.Environment)
	}
	if other.UserID != nil {
		w.UserID = copyPtr(other.UserID)
	}
	if other.User != "" {
		w.User = other.User
	}
	if other.GroupID != nil {
		w.GroupID = copyPtr(other.GroupID)
	}
	if other.Group != "" {
		w.Group = other.Group
	}
}

func (w *Workload) Equal(other *Workload) bool {
	return reflect.DeepEqual(w, other)
}

const WorkloadsField = "workloads"

var (
	_ plan.Section          = (*Workloads)(nil)
	_ plan.SectionExtension = (*Workloads)(nil)
)

type Workloads struct {
	Entries map[string]*Workload `yaml:",inline"`
}

func (w *Workloads) IsZero() bool {
	return len(w.Entries) == 0
}

func (w *Workloads) Validate() error {
	for name, workload := range w.Entries {
		if workload == nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q: cannot have a null value", name),
			}
		}
		if err := workload.validate(); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q: %v", name, err),
			}
		}
	}
	return nil
}

func (w *Workloads) combine(other *Workloads) error {
	for name, workload := range other.Entries {
		w.Entries = makeMapIfNil(w.Entries)
		switch workload.Override {
		case plan.MergeOverride:
			if current, ok := w.Entries[name]; ok {
				copied := current.copy()
				copied.merge(workload)
				w.Entries[name] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			w.Entries[name] = workload.copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q: must define an "override" policy`, name),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q: has an invalid "override" policy: %q`, name, workload.Override),
			}
		}
	}
	return nil
}

func (*Workloads) CombineSections(sections ...plan.Section) (plan.Section, error) {
	workloads := &Workloads{}
	for _, section := range sections {
		// The following will panic if any of the supplied section is not a WorkloadsSection
		layer := section.(*Workloads)
		if err := workloads.combine(layer); err != nil {
			return nil, err
		}
	}
	return workloads, nil
}

func (*Workloads) ParseSection(data yaml.Node) (plan.Section, error) {
	workloads := &Workloads{}
	if err := plan.SectionDecode(&data, workloads); err != nil {
		return nil, &plan.FormatError{
			Message: fmt.Sprintf(`cannot parse the "workloads" section: %v`, err),
		}
	}
	for name, workload := range workloads.Entries {
		if workload != nil {
			workload.Name = name
		}
	}
	return workloads, nil
}

func (*Workloads) ValidatePlan(p *plan.Plan) error {
	// The following will panic if the "ws" section is not a WorkloadsSection
	ws := p.Sections[WorkloadsField].(*Workloads)
	for name, service := range p.Services {
		if service.Workload == "" {
			continue
		}
		if _, ok := ws.Entries[service.Workload]; !ok {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q: not defined for service %q", service.Workload, name),
			}
		}
	}
	for name, workload := range ws.Entries {
		if _, _, err := osutil.NormalizeUidGid(workload.UserID, workload.GroupID, workload.User, workload.Group); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q: %v", name, err),
			}
		}
	}
	return nil
}
func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}

func makeMapIfNil[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		m = make(map[K]V)
	}
	return m
}
