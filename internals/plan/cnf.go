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
	"bytes"
	"encoding/json/v2"
	"fmt"

	"github.com/canonical/go-cnf/cnfjson"
)

type jsonLayer struct {
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Services    map[string]*Service    `json:"services"`
	Checks      map[string]*Check      `json:"checks"`
	LogTargets  map[string]*LogTarget  `json:"log-targets"`
	Sections    map[string]jsonSection `json:",embed"`
}

type unknownSection struct {
	data []byte
}

func (unknownSection) Validate() error {
	return nil
}

func (unknownSection) IsZero() bool {
	return false
}

type jsonSection struct {
	ext   SectionExtension
	value Section
}

func (s *jsonSection) UnmarshalJSON(data []byte) error {
	if s.ext == nil {
		s.value = unknownSection{
			data: data,
		}
		return nil
	}
	var err error
	s.value, err = s.ext.ParseSectionJSON(data)
	return err
}

// ParseLayerCNF parses a layer in CNF format.
func ParseLayerCNF(order int, label string, data []byte) (*Layer, error) {
	encoded, err := cnfjson.ToJSON(data, &cnfjson.Options{})
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}

	if encoded = bytes.TrimSpace(encoded); len(encoded) == 0 || encoded[0] != '{' {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: top-level value must be a map", label),
		}
	}

	raw := jsonLayer{
		Services:   make(map[string]*Service),
		Checks:     make(map[string]*Check),
		LogTargets: make(map[string]*LogTarget),
		Sections:   make(map[string]jsonSection),
	}
	for field, extension := range sectionExtensions {
		raw.Sections[field] = jsonSection{
			ext: extension,
		}
	}
	err = json.Unmarshal(encoded, &raw, json.RejectUnknownMembers(true))
	if err != nil {
		return nil, &FormatError{
			Message: fmt.Sprintf("cannot parse layer %q: %v", label, err),
		}
	}

	layer := &Layer{
		Summary:     raw.Summary,
		Description: raw.Description,
		Services:    raw.Services,
		Checks:      raw.Checks,
		LogTargets:  raw.LogTargets,
		Sections:    make(map[string]Section, len(sectionExtensions)),
	}
	for field, section := range raw.Sections {
		if _, ok := section.value.(unknownSection); ok {
			return nil, &FormatError{
				Message: fmt.Sprintf("cannot parse layer %q: unknown section %q", label, field),
			}
		}
		if section.value == nil {
			section.value, err = section.ext.ParseSectionJSON(nil)
			if err != nil {
				return nil, err
			}
		}
		layer.Sections[field] = section.value
	}

	return layer.finishParse(order, label)
}

// MarshalPlanCNF encodes a plan in CNF format using its JSON representation.
func MarshalPlanCNF(p *Plan) ([]byte, error) {
	encoded, err := json.Marshal(p, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return cnfjson.FromJSON(encoded, &cnfjson.Options{})
}

// SectionDecodeJSON decodes a JSON section into v.
// Unknown struct fields are rejected, matching SectionDecode's YAML behavior.
func SectionDecodeJSON(data []byte, v any) error {
	return json.Unmarshal(data, v, json.RejectUnknownMembers(true))
}
