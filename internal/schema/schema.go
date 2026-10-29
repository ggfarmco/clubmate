package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

type Object struct {
	Properties map[string]Prop `json:"properties"`
	Required   []string        `json:"required,omitempty"`
}

type Prop struct {
	Type        string          `json:"type"`
	Items       *Prop           `json:"items,omitempty"`
	Properties  map[string]Prop `json:"properties,omitempty"`
	Required    []string        `json:"required,omitempty"`
	Enum        []any           `json:"enum,omitempty"`
	Description string          `json:"description,omitempty"`
	Default     any             `json:"default,omitempty"`
}

var scalars = []string{"string", "integer", "number", "boolean"}

func Check(o *Object) error {
	if o == nil {
		return nil
	}
	return checkObject("", o.Properties, o.Required)
}

func checkObject(prefix string, props map[string]Prop, required []string) error {
	for _, name := range slices.Sorted(maps.Keys(props)) {
		if err := props[name].check(prefix + name); err != nil {
			return err
		}
	}
	for _, name := range required {
		if _, ok := props[name]; !ok {
			return fmt.Errorf("required %q is not a property", prefix+name)
		}
	}
	return nil
}

func (p Prop) checkItem(name string) error {
	if p.Type != "object" {
		return p.check(name)
	}
	if len(p.Enum) > 0 || p.Default != nil || p.Items != nil {
		return fmt.Errorf("%s: object items take only properties and required", name)
	}
	return checkObject(name+".", p.Properties, p.Required)
}

func (p Prop) check(name string) error {
	if len(p.Properties) > 0 || len(p.Required) > 0 {
		return fmt.Errorf("%s: properties on %s", name, p.Type)
	}
	switch {
	case p.Type == "array":
		if len(p.Enum) > 0 {
			return fmt.Errorf("%s: enum on array", name)
		}
		if p.Items == nil || p.Items.Type == "array" {
			return fmt.Errorf("%s: array needs scalar or object items", name)
		}
		if err := p.Items.checkItem(name + "[]"); err != nil {
			return err
		}
	case !slices.Contains(scalars, p.Type):
		return fmt.Errorf("%s: unsupported type %q", name, p.Type)
	case p.Items != nil:
		return fmt.Errorf("%s: items on %s", name, p.Type)
	}
	for _, e := range p.Enum {
		raw, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("%s: enum: %w", name, err)
		}
		if _, ok := decode(p.Type, raw); !ok {
			return fmt.Errorf("%s: enum value %s is not %s", name, raw, p.Type)
		}
	}
	if p.Default == nil {
		return nil
	}
	raw, err := json.Marshal(p.Default)
	if err != nil {
		return fmt.Errorf("%s: default: %w", name, err)
	}
	if err := p.validate("default", raw); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func Validate(o *Object, raw json.RawMessage) error {
	if o == nil {
		return nil
	}
	fields := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return errors.New("args: want object")
		}
	}
	return validateFields("args", o.Properties, o.Required, fields)
}

func validateFields(path string, props map[string]Prop, required []string, fields map[string]json.RawMessage) error {
	for _, name := range required {
		if isNull(fields[name]) {
			return fmt.Errorf("%s.%s: required", path, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(props)) {
		v, ok := fields[name]
		if !ok || isNull(v) {
			continue
		}
		if err := props[name].validate(path+"."+name, v); err != nil {
			return err
		}
	}
	return nil
}

func (p Prop) validate(path string, raw json.RawMessage) error {
	if p.Type == "object" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return fmt.Errorf("%s: want object", path)
		}
		return validateFields(path, p.Properties, p.Required, fields)
	}
	if p.Type == "array" {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return fmt.Errorf("%s: want array", path)
		}
		for i, item := range items {
			if err := p.Items.validate(fmt.Sprintf("%s[%d]", path, i), item); err != nil {
				return err
			}
		}
		return nil
	}
	v, ok := decode(p.Type, raw)
	if !ok {
		return fmt.Errorf("%s: want %s", path, p.Type)
	}
	if len(p.Enum) > 0 && !slices.Contains(p.Enum, v) {
		enum, _ := json.Marshal(p.Enum)
		return fmt.Errorf("%s: want one of %s", path, enum)
	}
	return nil
}

func decode(typ string, raw json.RawMessage) (any, bool) {
	if isNull(raw) {
		return nil, false
	}
	switch typ {
	case "string":
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err == nil
	case "boolean":
		var b bool
		err := json.Unmarshal(raw, &b)
		return b, err == nil
	case "integer":
		var n int64
		err := json.Unmarshal(raw, &n)
		return float64(n), err == nil
	case "number":
		var f float64
		err := json.Unmarshal(raw, &f)
		return f, err == nil
	}
	return nil, false
}

func isNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
