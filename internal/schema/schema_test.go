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
		},
		Required: []string{"path"},
	}
	tests := []struct {
		name   string
		schema *schema.Object
		raw    string
		err    string
	}{
		{name: "valid", schema: s, raw: `{"path":"/a","line":3,"side":"left","paths":["x"]}`},
		{name: "unknown field allowed", schema: s, raw: `{"path":"/a","extra":{"n":1}}`},
		{name: "missing required", schema: s, raw: `{"line":3}`, err: "args.path: required"},
		{name: "null args", schema: s, raw: `null`, err: "args.path: required"},
		{name: "wrong type", schema: s, raw: `{"path":"/a","line":"3"}`, err: "args.line: want integer"},
		{name: "fraction is not integer", schema: s, raw: `{"path":"/a","line":1.5}`, err: "args.line: want integer"},
		{name: "enum mismatch", schema: s, raw: `{"path":"/a","side":"up"}`, err: `args.side: want one of ["left","right"]`},
		{name: "array items typed", schema: s, raw: `{"path":"/a","paths":["x",1]}`, err: "args.paths[1]: want string"},
		{name: "not an object", schema: s, raw: `[1]`, err: "args: want object"},
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
		{name: "valid", schema: `{"properties":{"ids":{"type":"array","items":{"type":"integer"}}},"required":["ids"]}`},
		{name: "unknown type", schema: `{"properties":{"x":{"type":"object"}}}`, err: `x: unsupported type "object"`},
		{name: "array without items", schema: `{"properties":{"x":{"type":"array"}}}`, err: `x: array needs scalar items`},
		{name: "required not declared", schema: `{"properties":{},"required":["x"]}`, err: `required "x" is not a property`},
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
