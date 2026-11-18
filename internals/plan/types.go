package plan

import (
	"encoding/json/v2"
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

func (o OptionalDuration) MarshalYAML() (any, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Value.String(), nil
}

func (o *OptionalDuration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a YAML string")
	}
	return o.set(value.Value)
}

func (o OptionalDuration) MarshalJSON() ([]byte, error) {
	if !o.IsSet {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value.String())
}

func (o *OptionalDuration) UnmarshalJSON(data []byte) error {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return fmt.Errorf("duration must be a JSON string")
	}
	return o.set(*value)
}

func (o *OptionalDuration) set(value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q", value)
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

func (o OptionalFloat) MarshalYAML() (any, error) {
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

func (o OptionalFloat) MarshalJSON() ([]byte, error) {
	if !o.IsSet {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

func (o *OptionalFloat) UnmarshalJSON(data []byte) error {
	var value *float64
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return fmt.Errorf("value must be a JSON number")
	}
	o.Value = *value
	o.IsSet = true
	return nil
}
