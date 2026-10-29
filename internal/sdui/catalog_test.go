package sdui_test

import (
	"encoding/json"
	"strconv"
	"strings"
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
		{"list nil items", sdui.List("l", nil, ""), true},
		{"json null items", sdui.Node{Type: "list", ID: "l", Props: map[string]any{"items": nil}}, true},
		{"empty item id", sdui.List("l", []sdui.Item{{ID: "", Label: "A"}}, ""), false},
		{"duplicate item ids", sdui.List("l", []sdui.Item{{ID: "a", Label: "A"}, {ID: "a", Label: "B"}}, ""), false},
		{"button with action", sdui.WithAction(sdui.Button("b", "Go", ""), "tab.open"), true},
		{"bad action", sdui.WithAction(sdui.Button("b", "Go", ""), "Bad Action"), false},
		{"action too long", sdui.WithAction(sdui.Button("b", "Go", ""), "a"+strings.Repeat("b", 64)), false},
		{"action on text", sdui.WithAction(sdui.Text("a"), "go"), false},
		{"folderField", sdui.WithAction(sdui.FolderField("r", "Choose folders…", []string{"/a"}), "pick"), true},
		{"folderField without label", sdui.Node{Type: "folderField", ID: "r"}, false},
		{"folderField bad values", sdui.Node{Type: "folderField", ID: "r", Props: map[string]any{"label": "L", "values": []any{1}}}, false},
		{"tree muted", sdui.Tree("t", []sdui.TreeItem{{ID: "a", Label: "A", Muted: true}}, ""), true},
		{"tree breadcrumb", sdui.BreadcrumbTree("t", []sdui.TreeItem{{ID: "/r/a/x", Label: "x"}}, "", []sdui.Item{{ID: "/r", Label: "r"}, {ID: "/r/a", Label: "a"}}), true},
		{"tree muted not bool", sdui.Node{Type: "tree", ID: "t", Props: map[string]any{"items": []any{map[string]any{"id": "a", "label": "A", "muted": "yes"}}}}, false},
		{"diff", sdui.Diff("+a"), true},
		{"diff without text", sdui.Node{Type: "diff"}, false},
		{"diff with children", sdui.Node{Type: "diff", Props: map[string]any{"text": "x"}, Children: []sdui.Node{sdui.Text("a")}}, false},
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

func TestValidateItemLimits(t *testing.T) {
	items := make([]sdui.Item, 5001)
	for i := range items {
		items[i] = sdui.Item{ID: strconv.Itoa(i), Label: "x"}
	}
	assert.ErrorIs(t, internalsdui.Validate(sdui.List("l", items, "")), internalsdui.ErrInvalidTree)

	item := sdui.TreeItem{ID: "leaf", Label: "x"}
	for i := range 40 {
		item = sdui.TreeItem{ID: "n" + strconv.Itoa(i), Label: "x", Children: []sdui.TreeItem{item}}
	}
	assert.ErrorIs(t, internalsdui.Validate(sdui.Tree("t", []sdui.TreeItem{item}, "")), internalsdui.ErrInvalidTree)
}

func TestTableValidation(t *testing.T) {
	columns := []sdui.Column{{ID: "name", Title: "Name"}, {ID: "cpu", Title: "CPU", Align: "right", Width: 60}, {ID: "hist", Title: "History"}}
	half := 0.5
	over := 1.5
	row := func(id string, bar *float64, spark *sdui.Spark) sdui.TableRow {
		return sdui.TableRow{ID: id, Depth: 1, HasChildren: true, Expanded: true, Muted: true, Cells: []sdui.Cell{{Text: id}, {Text: "50%", Bar: bar, Tone: "warn"}, {Spark: spark}}}
	}
	rows := func(n, sparks int) []sdui.TableRow {
		out := make([]sdui.TableRow, n)
		for i := range out {
			var spark *sdui.Spark
			if i < sparks {
				spark = &sdui.Spark{Values: make([]float64, 900), Max: 100}
			}
			out[i] = row(strconv.Itoa(i), &half, spark)
		}
		return out
	}
	wide := make([]sdui.Column, 33)
	for i := range wide {
		wide[i] = sdui.Column{ID: strconv.Itoa(i), Title: "c"}
	}
	cpu := sdui.Sort{Column: "cpu", Desc: true}
	tests := []struct {
		name string
		node sdui.Node
		ok   bool
	}{
		{"valid", sdui.Table("t", columns, rows(1000, 3), "0", cpu), true},
		{"mismatched cells", sdui.Table("t", columns, []sdui.TableRow{{ID: "a", Cells: []sdui.Cell{{Text: "a"}}}}, "", sdui.Sort{}), false},
		{"bar over 1", sdui.Table("t", columns, []sdui.TableRow{row("a", &over, nil)}, "", sdui.Sort{}), false},
		{"too many rows", sdui.Table("t", columns, rows(1001, 0), "", sdui.Sort{}), false},
		{"too many columns", sdui.Table("t", wide, nil, "", sdui.Sort{}), false},
		{"unknown sort column", sdui.Table("t", columns, nil, "", sdui.Sort{Column: "mem"}), false},
		{"spark too long", sdui.Table("t", columns, []sdui.TableRow{row("a", nil, &sdui.Spark{Values: make([]float64, 901), Max: 1})}, "", sdui.Sort{}), false},
		{"spark zero max", sdui.Table("t", columns, []sdui.TableRow{row("a", nil, &sdui.Spark{Values: []float64{1}})}, "", sdui.Sort{}), false},
		{"too many spark values", sdui.Table("t", columns, rows(112, 112), "", sdui.Sort{}), false},
		{"sparkline", sdui.Sparkline("", []float64{1, 2}, 4), true},
		{"sparkline height out of range", sdui.SparklineSized("", []float64{1, 2}, 4, 201), false},
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
