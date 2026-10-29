package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/schema"
)

func TestSchemaValidate(t *testing.T) {
	s := &schema.Object{
		Properties: map[string]schema.Prop{
			"path":  {Type: "string"},
			"line":  {Type: "integer"},
			"side":  {Type: "string", Enum: []any{"left", "right"}},
			"paths": {Type: "array", Items: &schema.Prop{Type: "string"}},
			"items": {Type: "array", Items: &schema.Prop{Type: "object", Properties: map[string]schema.Prop{"path": {Type: "string"}, "line": {Type: "integer"}}, Required: []string{"path"}}},
		},
		Required: []string{"path"},
	}
	tests := []struct {
		name   string
		schema *schema.Object
		raw    string
		err    string
	}{
		{name: "valid", schema: s, raw: `{"path":"/a","line":3,"side":"left","paths":["x"],"items":[{"path":"/b","line":2}]}`},
		{name: "unknown field allowed", schema: s, raw: `{"path":"/a","extra":{"n":1}}`},
		{name: "missing required", schema: s, raw: `{"line":3}`, err: "args.path: required"},
		{name: "null args", schema: s, raw: `null`, err: "args.path: required"},
		{name: "wrong type", schema: s, raw: `{"path":"/a","line":"3"}`, err: "args.line: want integer"},
		{name: "fraction is not integer", schema: s, raw: `{"path":"/a","line":1.5}`, err: "args.line: want integer"},
		{name: "enum mismatch", schema: s, raw: `{"path":"/a","side":"up"}`, err: `args.side: want one of ["left","right"]`},
		{name: "array items typed", schema: s, raw: `{"path":"/a","paths":["x",1]}`, err: "args.paths[1]: want string"},
		{name: "not an object", schema: s, raw: `[1]`, err: "args: want object"},
		{name: "object item typed", schema: s, raw: `{"path":"/a","items":[{"path":"/b","line":"2"}]}`, err: "args.items[0].line: want integer"},
		{name: "object item required", schema: s, raw: `{"path":"/a","items":[{"path":"/b"},{"line":2}]}`, err: "args.items[1].path: required"},
		{name: "object item not an object", schema: s, raw: `{"path":"/a","items":[null]}`, err: "args.items[0]: want object"},
		{name: "nil schema accepts any object", raw: `{"anything":[1,"x"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.schema, json.RawMessage(tt.raw))
			if tt.err == "" {
				require.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.err)
		})
	}
}

func TestSchemaCheck(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		err    string
	}{
		{name: "valid", schema: `{"properties":{"ids":{"type":"array","items":{"type":"integer"},"default":[1]}},"required":["ids"]}`},
		{name: "unknown type", schema: `{"properties":{"x":{"type":"object","properties":{"p":{"type":"string"}}}}}`, err: `x: unsupported type "object"`},
		{name: "array without items", schema: `{"properties":{"x":{"type":"array"}}}`, err: `x: array needs scalar or object items`},
		{name: "required not declared", schema: `{"properties":{},"required":["x"]}`, err: `required "x" is not a property`},
		{name: "mistyped enum", schema: `{"properties":{"x":{"type":"integer","enum":[1,"two"]}}}`, err: `x: enum value "two" is not integer`},
		{name: "mistyped default", schema: `{"properties":{"x":{"type":"boolean","default":"yes"}}}`, err: `x: default: want boolean`},
		{name: "enum on array", schema: `{"properties":{"x":{"type":"array","items":{"type":"string"},"enum":["a"]}}}`, err: `x: enum on array`},
		{name: "object items", schema: `{"properties":{"x":{"type":"array","items":{"type":"object","properties":{"p":{"type":"string"}},"required":["p"]}}}}`},
		{name: "object item properties checked", schema: `{"properties":{"x":{"type":"array","items":{"type":"object","properties":{"p":{"type":"date"}}}}}}`, err: `x[].p: unsupported type "date"`},
		{name: "array of arrays", schema: `{"properties":{"x":{"type":"array","items":{"type":"array","items":{"type":"string"}}}}}`, err: `x: array needs scalar or object items`},
		{name: "properties on scalar", schema: `{"properties":{"x":{"type":"string","properties":{"p":{"type":"string"}}}}}`, err: `x: properties are allowed only on object items, not on string`},
		{name: "object items without properties", schema: `{"properties":{"x":{"type":"array","items":{"type":"object"}}}}`, err: `x[]: object items need properties`},
		{name: "object items nested", schema: `{"properties":{"x":{"type":"array","items":{"type":"object","properties":{"y":{"type":"array","items":{"type":"object","properties":{"p":{"type":"string"}}}}}}}}}`, err: `x[].y: object items nest one level only`},
		{name: "items on scalar", schema: `{"properties":{"x":{"type":"string","items":{"type":"string"}}}}`, err: `x: items on string`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o schema.Object
			require.NoError(t, json.Unmarshal([]byte(tt.schema), &o))
			err := schema.Check(&o)
			if tt.err == "" {
				require.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.err)
		})
	}
}
