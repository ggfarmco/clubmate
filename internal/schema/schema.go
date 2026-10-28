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
	Type        string `json:"type"`
	Items       *Prop  `json:"items,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
}

var scalars = []string{"string", "integer", "number", "boolean"}

func Check(o *Object) error {
	if o == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(o.Properties)) {
		p := o.Properties[name]
		if p.Type == "array" {
			if p.Items == nil || p.Items.Type == "array" {
				return fmt.Errorf("%s: array needs scalar items", name)
			}
			p = *p.Items
		}
		if err := p.check(name); err != nil {
			return err
		}
	}
	for _, name := range o.Required {
		if _, ok := o.Properties[name]; !ok {
			return fmt.Errorf("required %q is not a property", name)
		}
	}
	return nil
}

func (p Prop) check(name string) error {
	if !slices.Contains(scalars, p.Type) {
		return fmt.Errorf("%s: unsupported type %q", name, p.Type)
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
	for _, name := range o.Required {
		if isNull(fields[name]) {
			return fmt.Errorf("args.%s: required", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(o.Properties)) {
		v, ok := fields[name]
		if !ok || isNull(v) {
			continue
		}
		if err := o.Properties[name].validate("args."+name, v); err != nil {
			return err
		}
	}
	return nil
}

func (p Prop) validate(path string, raw json.RawMessage) error {
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
