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

var _ plan.SectionExtension = (*PlanExtension)(nil)

type PlanExtension struct{}

func (ext *PlanExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
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

func (ext *PlanExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	ws := &WorkloadsSection{}
	// The following issue prevents us from using the yaml.Node decoder
	// with KnownFields = true behavior. Once one of the proposals get
	// merged, we can remove the intermediate Marshal step.
	// https://github.com/go-yaml/yaml/issues/460
	if len(data.Content) != 0 {
		if err := plan.SectionDecode(&data, ws); err != nil {
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

func (ext *PlanExtension) ValidatePlan(p *plan.Plan) error {
	ws, ok := p.Sections[WorkloadsField].(*WorkloadsSection)
	if !ok {
		return fmt.Errorf("internal error: invalid section type %T", ws)
	}
	for name, service := range p.Services {
		if service.Workload == "" {
			continue
		}
		if _, ok := ws.Entries[service.Workload]; !ok {
			return &plan.FormatError{
				Message: fmt.Sprintf(`plan service %q workload not defined: %q`, name, service.Workload),
			}
		}
	}
	for name, workload := range ws.Entries {
		if _, _, err := osutil.NormalizeUidGid(workload.UserID, workload.GroupID, workload.User, workload.Group); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf(`plan workload %q %v`, err, name),
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
				Message: fmt.Sprintf("workload %q cannot have a null value", name),
			}
		}
		if err := workload.Validate(); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("workload %q %v", name, err),
			}
		}
	}
	return nil
}

func (ws *WorkloadsSection) combine(other *WorkloadsSection) error {
	for name, workload := range other.Entries {
		if ws.Entries == nil {
			ws.Entries = make(map[string]*Workload, len(other.Entries))
		}
		switch workload.Override {
		case plan.MergeOverride:
			if current, ok := ws.Entries[name]; ok {
				copied := current.Copy()
				copied.Merge(workload)
				ws.Entries[name] = copied
				break
			}
			fallthrough
		case plan.ReplaceOverride:
			ws.Entries[name] = workload.Copy()
		case plan.UnknownOverride:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q must define an "override" policy`, name),
			}
		default:
			return &plan.FormatError{
				Message: fmt.Sprintf(`workload %q has an invalid "override" policy: %q`, name, workload.Override),
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

func (w *Workload) Validate() error {
	if w.Name == "" {
		return errors.New("cannot have an empty name")
	}
	// Value of Override is checked in the (*WorkloadSection).combine() method
	return nil
}

func (w *Workload) Copy() *Workload {
	copied := *w
	copied.Environment = maps.Clone(w.Environment)
	copied.UserID = copyPtr(w.UserID)
	copied.GroupID = copyPtr(w.GroupID)
	return &copied
}

func (w *Workload) Merge(other *Workload) {
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
	if !maps.Equal(w.Environment, other.Environment) {
		return false
	}

	uid, gid, err := osutil.NormalizeUidGid(w.UserID, w.GroupID, w.User, w.Group)
	if err != nil {
		return reflect.DeepEqual(w, other)
	}
	otherUID, otherGID, err := osutil.NormalizeUidGid(other.UserID, other.GroupID, other.User, other.Group)
	if err != nil {
		return reflect.DeepEqual(w, other)
	}
	if uid != nil && gid != nil && otherUID != nil && otherGID != nil {
		return *uid == *otherUID && *gid == *otherGID
	}
	return reflect.DeepEqual(w, other)
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
