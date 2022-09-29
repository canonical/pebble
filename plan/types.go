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
	"fmt"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type OptionalDuration struct {
	Value time.Duration
	IsSet bool
}

func (o OptionalDuration) IsZero() bool {
	return !o.IsSet
}

func (o OptionalDuration) MarshalYAML() (interface{}, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Value.String(), nil
}

func (o *OptionalDuration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a YAML string")
	}
	duration, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q", value.Value)
	}
	o.Value = duration
	o.IsSet = true
	return nil
}

type OptionalFloat struct {
	Value float64
	IsSet bool
}

func (o OptionalFloat) IsZero() bool {
	return !o.IsSet
}

func (o OptionalFloat) MarshalYAML() (interface{}, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Value, nil
}

func (o *OptionalFloat) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("value must be a YAML number")
	}
	n, err := strconv.ParseFloat(value.Value, 64)
	if err != nil {
		return fmt.Errorf("invalid floating-point number %q", value.Value)
	}
	o.Value = n
	o.IsSet = true
	return nil
}
