package sdui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

const sesID = "ses_0123456789abcdef"

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		node sdui.Node
		ok   bool
	}{
		{"list ok", sdui.List("t", []sdui.Item{{ID: "a", Label: "A"}}, "a"), true},
		{"unknown type", sdui.Node{Type: "marquee"}, false},
		{"interactive without id", sdui.Button("", "New", "plus"), false},
		{"duplicate ids", sdui.VStack(sdui.Button("x", "A", ""), sdui.Button("x", "B", "")), false},
		{"bad surface id", sdui.Surface("../x"), false},
		{"good surface id", sdui.Surface(sesID), true},
		{"unknown prop", sdui.Node{Type: "text", Props: map[string]any{"text": "a", "color": "red"}}, false},
		{"missing prop", sdui.Node{Type: "list", ID: "l"}, false},
		{"bad enum", sdui.Badge("x", "pink"), false},
		{"split needs two children", sdui.Split("vertical", sdui.Text("a")), false},
		{"scroll needs one child", sdui.Node{Type: "scroll"}, false},
		{"unknown item field", sdui.Node{Type: "list", ID: "l", Props: map[string]any{"items": []any{map[string]any{"id": "a", "label": "A", "x": 1}}}}, false},
		{"tree nested", sdui.Tree("t", []sdui.TreeItem{{ID: "a", Label: "A", Children: []sdui.TreeItem{{ID: "b", Label: "B"}}}}, ""), true},
		{"tree bad nested", sdui.Node{Type: "tree", ID: "t", Props: map[string]any{"items": []any{map[string]any{"id": "a", "label": "A", "children": []any{map[string]any{"id": "b"}}}}}}, false},
		{"toggle false", sdui.Toggle("t", false), true},
		{"select", sdui.Select("s", []sdui.Option{{Value: "a", Label: "A"}}, "a"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := internalsdui.Validate(tt.node)
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, internalsdui.ErrInvalidTree)
		})
	}
}

func TestValidateAcceptsJSONDecodedTree(t *testing.T) {
	raw, err := json.Marshal(sdui.VStack(sdui.List("l", []sdui.Item{{ID: "a", Label: "A"}}, "a")))
	require.NoError(t, err)
	var n sdui.Node
	require.NoError(t, json.Unmarshal(raw, &n))
	assert.NoError(t, internalsdui.Validate(n))
}

func TestValidateErrorPath(t *testing.T) {
	err := internalsdui.Validate(sdui.VStack(sdui.Node{Type: "list", ID: "l"}))
	assert.ErrorContains(t, err, "stack[0].list: missing prop items")
}

func TestValidateLimits(t *testing.T) {
	deep := sdui.Text("x")
	for range 32 {
		deep = sdui.VStack(deep)
	}
	assert.ErrorIs(t, internalsdui.Validate(deep), internalsdui.ErrInvalidTree)

	wide := make([]sdui.Node, 5000)
	for i := range wide {
		wide[i] = sdui.Text("x")
	}
	assert.ErrorIs(t, internalsdui.Validate(sdui.VStack(wide...)), internalsdui.ErrInvalidTree)
}
