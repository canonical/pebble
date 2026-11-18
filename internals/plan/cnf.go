// Copyright (c) 2026 Canonical Ltd
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
	"encoding/json/v2"
	"fmt"
	"slices"

	"github.com/canonical/go-cnf/cnfmap"
)

// ParseLayerCNF parses a layer in CNF format.
func ParseLayerCNF(order int, label string, data []byte) (*Layer, error) {
	decoded, err := cnfmap.Unmarshal(data)
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}
	sections, ok := decoded.(map[string]any)
	if !ok {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: top-level value must be a map", label),
		}
	}

	layer := &Layer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
		Sections:   make(map[string]Section),
	}
	builtins := map[string]any{
		"summary":     &layer.Summary,
		"description": &layer.Description,
		"services":    &layer.Services,
		"checks":      &layer.Checks,
		"log-targets": &layer.LogTargets,
	}

	// Give every extension an empty value, matching the empty yaml.Node passed
	// to extensions when parsing YAML layers without the section.
	for field := range sectionExtensions {
		if _, ok := sections[field]; !ok {
			sections[field] = nil
		}
	}

	for field, section := range sections {
		if slices.Contains(builtinSections, field) {
			if err := SectionDecodeCNF(section, builtins[field]); err != nil {
				return nil, &FormatError{
					Message: fmt.Sprintf("cannot parse layer %q section %q: %v", label, field, err),
				}
			}
			continue
		}

		extension, ok := sectionExtensions[field]
		if !ok {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot parse layer %q: unknown section %q", label, field),
			}
		}
		layer.Sections[field], err = extension.ParseSectionCNF(section)
		if err != nil {
			return nil, err
		}
	}

	return layer.finishParse(order, label)
}

// SectionDecodeCNF decodes a generic value returned by cnfmap.Unmarshal into v.
// Unknown struct fields are rejected, matching SectionDecode's YAML behavior.
func SectionDecodeCNF(data any, v any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("cannot encode CNF data as JSON: %w", err)
	}
	return json.Unmarshal(encoded, v, json.RejectUnknownMembers(true))
}
