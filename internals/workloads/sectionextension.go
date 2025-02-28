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
	"fmt"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/plan"
	"gopkg.in/yaml.v3"
)

var _ plan.SectionExtension = (*SectionExtension)(nil)

type SectionExtension struct{}

func (ext *SectionExtension) CombineSections(sections ...plan.Section) (plan.Section, error) {
	ws := &WorkloadsSection{}
	for _, section := range sections {
		// The following will panic if any of the supplied section is not a WorkloadsSection
		layer := section.(*WorkloadsSection)
		if err := ws.combine(layer); err != nil {
			return nil, err
		}
	}
	return ws, nil
}

func (ext *SectionExtension) ParseSection(data yaml.Node) (plan.Section, error) {
	ws := &WorkloadsSection{}
	if err := plan.SectionDecode(&data, ws); err != nil {
		return nil, &plan.FormatError{
			Message: fmt.Sprintf(`cannot parse the "workloads" section: %v`, err),
		}
	}
	for name, workload := range ws.Entries {
		if workload != nil {
			workload.Name = name
		}
	}
	return ws, nil
}

func (ext *SectionExtension) ValidatePlan(p *plan.Plan) error {
	// The following will panic if the "workloads" section is not a WorkloadsSection
	ws := p.Sections[WorkloadsField].(*WorkloadsSection)
	for name, service := range p.Services {
		if service.Workload == "" {
			continue
		}
		if _, ok := ws.Entries[service.Workload]; !ok {
			return &plan.FormatError{
				Message: fmt.Sprintf("plan service %q workload not defined: %q", name, service.Workload),
			}
		}
	}
	for name, workload := range ws.Entries {
		if _, _, err := osutil.NormalizeUidGid(workload.UserID, workload.GroupID, workload.User, workload.Group); err != nil {
			return &plan.FormatError{
				Message: fmt.Sprintf("plan workload %q %v", err, name),
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
