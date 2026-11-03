package sdui_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

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
		{"checkbox", sdui.Checkbox("t", "tests 3", true), true},
		{"toggle bad style", sdui.Node{Type: "toggle", ID: "t", Props: map[string]any{"value": true, "style": "radio"}}, false},
		{"progress", sdui.Progress("p", 4, 15, "success"), true},
		{"progress negative", sdui.Progress("p", -1, 15, ""), false},
		{"tree item badges and changes", sdui.Tree("t", []sdui.TreeItem{{ID: "a", Label: "a.go", Badges: []sdui.HeaderBadge{{Text: "new", Tone: "success"}, {Text: "1", Tone: "warning"}}, Changes: &sdui.Changes{Added: 10}, Menu: []sdui.MenuEntry{{Action: "toggleReviewed", Label: "Mark reviewed", Key: "r"}}}}, ""), true},
		{"tree item changes without removed", sdui.Node{Type: "tree", ID: "t", Props: map[string]any{"items": []any{map[string]any{"id": "a", "label": "A", "changes": map[string]any{"added": 1}}}}}, false},
		{"menu key two letters", sdui.List("l", []sdui.Item{{ID: "a", Label: "A", Menu: []sdui.MenuEntry{{Action: "go", Label: "Go", Key: "rr"}}}}, ""), false},
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
		{"tree item truncate", sdui.Tree("t", []sdui.TreeItem{{ID: "a", Label: "feat/a", Truncate: "middle"}}, ""), true},
		{"tabs item tone", sdui.Tabs("f", []sdui.Item{{ID: "a", Label: "Needs you", Badge: "3", Tone: "warning"}}, "a"), true},
		{"unknown item tone", sdui.Tabs("f", []sdui.Item{{ID: "a", Label: "A", Tone: "red"}}, "a"), false},
		{"unknown item truncate", sdui.List("l", []sdui.Item{{ID: "a", Label: "A", Truncate: "start"}}, ""), false},
		{"tree faint", sdui.Tree("t", []sdui.TreeItem{{ID: "a", Label: "detached · 03a9ff5f", Faint: true}}, ""), true},
		{"text lines and tone", sdui.WithTone(sdui.WithLines(sdui.Text("a"), 3), "warning"), true},
		{"text bad tone", sdui.WithTone(sdui.Text("a"), "amber"), false},
		{"icon tone", sdui.WithTone(sdui.Icon("play.circle"), "accent"), true},
		{"icon bad tone", sdui.WithTone(sdui.Icon("play.circle"), "amber"), false},
		{"button overflow", sdui.WithOverflow(sdui.Button("b", "Delete", "")), true},
		{"tree muted not bool", sdui.Node{Type: "tree", ID: "t", Props: map[string]any{"items": []any{map[string]any{"id": "a", "label": "A", "muted": "yes"}}}}, false},
		{"diff", sdui.Diff("+a"), true},
		{"diff without text", sdui.Node{Type: "diff"}, false},
		{"diff with children", sdui.Node{Type: "diff", Props: map[string]any{"text": "x"}, Children: []sdui.Node{sdui.Text("a")}}, false},
		{"textField lines", sdui.WithLines(sdui.TextField("f", "", ""), 4), true},
		{"textField lines too small", sdui.WithLines(sdui.TextField("f", "", ""), 1), false},
		{"textField lines too large", sdui.WithLines(sdui.TextField("f", "", ""), 51), false},
		{"textField lines not a number", sdui.Node{Type: "textField", ID: "f", Props: map[string]any{"lines": "x"}}, false},
		{"select", sdui.Select("s", []sdui.Option{{Value: "a", Label: "A"}}, "a"), true},
		{"button variant, shortcut, disabled, icon only", sdui.WithIconOnly(sdui.WithDisabled(sdui.WithShortcut(sdui.WithVariant(sdui.Button("b", "Run", ""), sdui.VariantPrimary), "cmd+shift+r"))), true},
		{"button loading", sdui.WithLoading(sdui.Button("b", "Save", ""), "Saving…"), true},
		{"button with a popover form", sdui.WithPopover(sdui.Button("b", "Start", ""), sdui.Form("f", sdui.Button("go", "Go", ""))), true},
		{"button with a non-form child", sdui.Node{Type: "button", ID: "b", Props: map[string]any{"label": "Go"}, Children: []sdui.Node{sdui.Text("a")}}, false},
		{"text field drop", sdui.WithDrop(sdui.TextField("t", "", "Drop files")), true},
		{"field errors", sdui.Form("f", sdui.WithError(sdui.TextField("t", "", ""), "Title is empty"), sdui.WithError(sdui.Select("s", nil, ""), "Pick one")), true},
		{"button bad variant", sdui.WithVariant(sdui.Button("b", "Run", ""), "loud"), false},
		{"button bad shortcut", sdui.WithShortcut(sdui.Button("b", "Run", ""), "hyper+r"), false},
		{"header", sdui.Header("Nightly audit", sdui.HeaderProps{Icon: "clock", Meta: "every day", Badges: []sdui.HeaderBadge{{Text: "failed", Tone: "danger"}}}, sdui.Button("j/run", "Run now", "")), true},
		{"header bad badge tone", sdui.Header("x", sdui.HeaderProps{Badges: []sdui.HeaderBadge{{Text: "a", Tone: "pink"}}}), false},
		{"header with a text child", sdui.Header("x", sdui.HeaderProps{}, sdui.Text("a")), false},
		{"header with a live filter field", sdui.Header("x", sdui.HeaderProps{}, sdui.WithLive(sdui.TextField("q", "", "Filter")), sdui.WithCopy(sdui.Button("c", "Copy", ""), "ls -la")), true},
		{"header editing its title in a form", sdui.Form("f", sdui.Header("Fix login", sdui.HeaderProps{Field: "title", Error: "Title is empty"})), true},
		{"header with an empty field", sdui.Node{Type: "header", Props: map[string]any{"title": "x", "field": ""}}, false},
		{"header with two text fields", sdui.Header("x", sdui.HeaderProps{}, sdui.TextField("a", "", ""), sdui.TextField("b", "", "")), false},
		{"button reserved shortcut", sdui.WithShortcut(sdui.Button("b", "Run", ""), "shift+return"), false},
		{"empty state with action", sdui.EmptyState("Nothing yet.", sdui.EmptyProps{Title: "No runs", Icon: "clock", Hint: "cm cron add", Action: &sdui.Node{Type: "button", ID: "run", Props: map[string]any{"label": "Run"}}}), true},
		{"document", sdui.Document("d", sdui.DocumentProps{HTML: "<p>a</p>", Path: "/r/a.md", Rev: 3, Size: sdui.SizeFit, Anchor: "intro"}), true},
		{"document without html", sdui.Node{Type: "document", ID: "d", Props: map[string]any{"path": "/r/a.md"}}, false},
		{"document bad size", sdui.Document("d", sdui.DocumentProps{HTML: "x", Size: "big"}), false},
		{"document artifact", sdui.Document("d", sdui.DocumentProps{HTML: "<html></html>", Kind: sdui.DocumentArtifact}), true},
		{"document bad kind", sdui.Document("d", sdui.DocumentProps{HTML: "x", Kind: "x"}), false},
		{"document negative rev", sdui.Document("d", sdui.DocumentProps{HTML: "x", Rev: -1}), false},
		{"document without id", sdui.Document("", sdui.DocumentProps{HTML: "x"}), false},
		{"empty state with a non-button child", sdui.Node{Type: "empty", Props: map[string]any{"text": "x"}, Children: []sdui.Node{sdui.Text("a")}}, false},
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

func TestValidateAgentNodes(t *testing.T) {
	extra := func(n sdui.Node) sdui.Node {
		props := map[string]any{"bogus": 1}
		for k, v := range n.Props {
			props[k] = v
		}
		n.Props = props
		return n
	}
	linked := sdui.Message("agent", "**hi** `a.md`", nil)
	linked.Props["docLinks"] = map[string]string{"a.md": "/r/a.md"}
	linked.Props["docCards"] = []sdui.DocCard{{Path: "/r/a.md", Kind: "markdown", Title: "A", Rel: "a.md"}, {Path: "/r/f.mmd", Kind: "mermaid", Title: "f", Rel: "f.mmd", Presented: true}}
	attachments := []sdui.Attachment{{Kind: "file", Path: "a.go"}, {ID: "s1", Kind: "selection", Path: "b.go", StartLine: 12, EndLine: 40}, {Kind: "image", Path: "image"}}
	nodes := map[string]sdui.Node{
		"transcript": sdui.Transcript("t", linked),
		"message":    sdui.Message("user", "fix it", attachments),
		"plan":       sdui.Plan([]sdui.PlanEntry{{Content: "Read", Status: "completed"}, {Content: "Edit", Status: "in_progress"}}),
		"toolCard": sdui.ToolCard("c", sdui.ToolCardProps{
			Kind: "edit", Title: "Edit a.go", Status: "completed", Body: &sdui.ToolBody{Format: "diff", Text: "+a"}, Truncated: true,
			Actions: []sdui.CardAction{{Action: "openLocation", Label: "Open in nvim", Value: map[string]any{"path": "/r/a.go", "line": 3}}, {Action: "showAll", Label: "Show all"}},
		}),
		"card":           sdui.Card("warning", "questionmark.bubble.fill", sdui.Text("Which store?")),
		"permissionCard": sdui.PermissionCard("p", "permission-1", sdui.PermissionCardProps{Text: "Run go test?", Title: "Run a command?", Command: "go test ./...", Caption: "Claude says: Run the tests", Options: []sdui.PermissionOption{{ID: "allow", Label: "Allow once", Kind: "allow_once", Shortcut: "opt+1"}, {ID: "always", Label: "Always allow…", Kind: "allow_always", Shortcut: "opt+2", Rule: "Bash(go test:*)", Scope: "in shop on this Mac"}, {ID: "never", Label: "Always reject", Kind: "reject_always", Menu: true}}}),
		"composer": sdui.Composer("m", sdui.ComposerProps{
			Placeholder: "Ask", Agent: "Claude", Modes: []sdui.Option{{Value: "default", Label: "Manual"}}, Mode: "default",
			Pickers:  []sdui.Picker{{ID: "model", Help: "Model", Value: "opus", Options: []sdui.Option{{Value: "opus", Label: "Opus"}}}},
			Commands: []sdui.Command{{Name: "review", Description: "Review"}}, Attach: []string{"file", "selection"}, Attachments: attachments, Running: true,
		}),
		"menu": sdui.List("l", []sdui.Item{{ID: "a", Label: "A", Menu: []sdui.MenuEntry{{Action: "delete", Label: "Delete", Confirm: "Sure?"}}}}, ""),
	}
	for name, n := range nodes {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, internalsdui.Validate(n))
			if n.Type != "list" {
				assert.ErrorIs(t, internalsdui.Validate(extra(n)), internalsdui.ErrInvalidTree)
			}
		})
	}
	invalid := map[string]sdui.Node{
		"bad role":               sdui.Message("system", "x", nil),
		"bad doc link":           {Type: "message", Props: map[string]any{"role": "agent", "text": "x", "docLinks": map[string]any{"a.md": 1}}},
		"bad doc card kind":      {Type: "message", Props: map[string]any{"role": "agent", "text": "x", "docCards": []any{map[string]any{"path": "/r/a.go", "kind": "code", "title": "a", "rel": "a.go"}}}},
		"bad doc card presented": {Type: "message", Props: map[string]any{"role": "agent", "text": "x", "docCards": []any{map[string]any{"path": "/r/a.md", "kind": "markdown", "title": "a", "rel": "a.md", "presented": "yes"}}}},
		"bad plan status":        sdui.Plan([]sdui.PlanEntry{{Content: "x", Status: "done"}}),
		"card without id":        sdui.ToolCard("", sdui.ToolCardProps{Kind: "read", Title: "x"}),
		"bad body format":        sdui.ToolCard("c", sdui.ToolCardProps{Kind: "read", Title: "x", Body: &sdui.ToolBody{Format: "html", Text: "x"}}),
		"nested action value":    sdui.ToolCard("c", sdui.ToolCardProps{Kind: "read", Title: "x", Actions: []sdui.CardAction{{Action: "go", Label: "Go", Value: map[string]any{"a": []int{1}}}}}),
		"empty card":             sdui.Card("warning", ""),
		"bad card tone":          sdui.Card("orange", "", sdui.Text("x")),
		"duplicate option":       sdui.PermissionCard("p", "r", sdui.PermissionCardProps{Text: "x", Options: []sdui.PermissionOption{{ID: "a", Label: "A"}, {ID: "a", Label: "B"}}}),
		"bad attach kind":        sdui.Composer("m", sdui.ComposerProps{Attach: []string{"image"}}),
		"duplicate attachment":   sdui.Composer("m", sdui.ComposerProps{Attachments: []sdui.Attachment{{ID: "a1", Kind: "file", Path: "a"}, {ID: "a1", Kind: "file", Path: "b"}}}),
		"menu bad action":        sdui.List("l", []sdui.Item{{ID: "a", Label: "A", Menu: []sdui.MenuEntry{{Action: "Bad Action", Label: "x"}}}}, ""),
	}
	for name, n := range invalid {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, internalsdui.Validate(n), internalsdui.ErrInvalidTree)
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

func TestWebViewNode(t *testing.T) {
	const page = "http://127.0.0.1:5000/?tkn=a&folder=%2Fx"
	webview := func(props map[string]any) sdui.Node { return sdui.Node{Type: "webview", Props: props} }
	tests := []struct {
		name string
		node sdui.Node
		ok   bool
	}{
		{"loopback", sdui.WebView("code", page, 0), true},
		{"rev", sdui.WebView("code", page, 3), true},
		{"https", sdui.WebView("code", "https://127.0.0.1:5000/", 0), false},
		{"localhost", sdui.WebView("code", "http://localhost:5000/", 0), false},
		{"lan host", sdui.WebView("code", "http://10.0.0.1:5000/", 0), false},
		{"no port", sdui.WebView("code", "http://127.0.0.1/", 0), false},
		{"file", sdui.WebView("code", "file:///x", 0), false},
		{"missing key", webview(map[string]any{"url": page}), false},
		{"unknown prop", webview(map[string]any{"key": "code", "url": page, "zoom": 2}), false},
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
	assert.NotContains(t, sdui.WebView("code", page, 0).Props, "rev")
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
	columns := []sdui.Column{{ID: "name", Title: "Name", Truncate: "middle"}, {ID: "cpu", Title: "CPU", Align: "right", Width: 60, Priority: 1}, {ID: "hist", Title: "History", Priority: 2}}
	half := 0.5
	over := 1.5
	row := func(id string, bar *float64, spark *sdui.Spark) sdui.TableRow {
		return sdui.TableRow{ID: id, Depth: 1, HasChildren: true, Expanded: true, Muted: true, Group: true, Cells: []sdui.Cell{{Text: id}, {Text: "50%", Bar: bar, Tone: "warn"}, {Spark: spark}}}
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
		{"unknown truncate", sdui.Table("t", []sdui.Column{{ID: "a", Title: "A", Truncate: "start"}}, nil, "", sdui.Sort{}), false},
		{"negative priority", sdui.Table("t", []sdui.Column{{ID: "a", Title: "A", Priority: -1}}, nil, "", sdui.Sort{}), false},
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

func TestGraphEdgeStates(t *testing.T) {
	graph := func(state string) sdui.Node {
		return sdui.Graph("run", sdui.GraphProps{
			Nodes: []sdui.GraphNode{{ID: "a", Label: "A", Outs: []sdui.GraphOut{{ID: "done", Label: "done", State: "plain"}}}},
			Edges: []sdui.GraphEdge{{From: "a", Out: "done", To: "a", State: state, Ref: "trg_1", Tooltip: "when A is done → prompt A"}},
		})
	}
	assert.NoError(t, internalsdui.Validate(graph("plain")))
	assert.ErrorContains(t, internalsdui.Validate(graph("maybe")), "prop edges: [0]: state")
}

func TestFlowNode(t *testing.T) {
	flow := func(edge sdui.FlowEdge) sdui.Node {
		return sdui.Flow("does", sdui.FlowProps{
			Cards:   []sdui.FlowCard{{ID: "check", Label: "Check CI", Outs: []sdui.FlowOut{{ID: "ok", Label: "ok", Count: 19, State: sdui.EdgeTaken}}}},
			Markers: []sdui.FlowMarker{{ID: "when", Kind: sdui.MarkerSchedule, Label: "every 2 min"}, {ID: "inbox", Kind: sdui.MarkerInbox, Label: "Inbox"}},
			Edges:   []sdui.FlowEdge{{From: "when", To: "check", State: sdui.EdgeTaken}, edge},
		})
	}
	assert.NoError(t, internalsdui.Validate(flow(sdui.FlowEdge{From: "check", Out: "ok", To: "inbox", State: sdui.EdgeSelected, Ref: "trg_1", Tooltip: "ok → inbox"})))
	assert.ErrorContains(t, internalsdui.Validate(flow(sdui.FlowEdge{From: "check", Out: "ok", To: "inbox", State: "maybe"})), "prop edges: [1]: state")
	ghost := sdui.Flow("empty", sdui.FlowProps{Cards: []sdui.FlowCard{{ID: "a", Label: "Action", Ghost: true}}, Markers: []sdui.FlowMarker{{ID: "k", Kind: "clock"}}})
	assert.ErrorContains(t, internalsdui.Validate(ghost), "prop markers: [0]: kind")
}

func TestLanesNode(t *testing.T) {
	lanes := func(cause int) sdui.Node {
		return sdui.Lanes("owners", sdui.LanesProps{
			Actors: []sdui.LaneActor{{ID: "you", Label: "You", Icon: "person"}, {ID: "s1", Label: "Coder", Tone: "accent"}},
			Moves:  []sdui.LaneMove{{Actor: "you", Cause: -1, Tooltip: "created"}, {Actor: "s1", Cause: cause, Label: "Start agent", Tooltip: "started", Last: true, Ref: "agt_1"}},
			Title:  "Owners",
		})
	}
	assert.NoError(t, internalsdui.Validate(lanes(0)))
	assert.ErrorContains(t, internalsdui.Validate(lanes(-2)), "prop moves: [1]: cause")
}

func TestRunstripNode(t *testing.T) {
	at := time.Date(2026, 11, 3, 16, 20, 0, 0, time.UTC)
	strip := sdui.Runstrip("runs", sdui.RunstripProps{
		Runs:     []sdui.StripRun{{At: at, Status: sdui.RunOK, Ref: "r1"}, {At: at.Add(2 * time.Minute), Status: sdui.RunFailed, Tooltip: "exit 1", Ref: "r2"}},
		Now:      at.Add(5 * time.Minute),
		Next:     at.Add(6 * time.Minute),
		Paused:   []sdui.PausedSpan{{From: at.Add(3 * time.Minute)}},
		Selected: "r2",
	})
	assert.NoError(t, internalsdui.Validate(strip))
	strip.Props["now"] = "16:25"
	assert.ErrorContains(t, internalsdui.Validate(strip), "prop now: must be an RFC 3339 time")
}

func TestGraphStepProps(t *testing.T) {
	graph := func(kind string) sdui.Node {
		nodes := []sdui.GraphNode{{ID: "tests", Label: "Tests", State: "running", Kind: kind, Command: "make test", Since: "2026-11-03T16:40:00Z", Timeout: 600, Tag: "changed", Outs: []sdui.GraphOut{{ID: "fail", Label: "fail", State: "taken"}}}}
		for i := range 15 {
			nodes = append(nodes, sdui.GraphNode{ID: "n" + strconv.Itoa(i), Label: "N", Kind: sdui.KindAgent})
		}
		return sdui.Graph("run", sdui.GraphProps{
			Nodes: nodes,
			Edges: []sdui.GraphEdge{{From: "tests", Out: "fail", To: "n0", State: sdui.EdgeHeld, Label: "×2/2 · waits for you"}},
		}, sdui.Card("warning", "", sdui.Text("Tests → Coder ran 2 of 2 rounds")))
	}
	assert.NoError(t, internalsdui.Validate(graph(sdui.KindCheck)))
	assert.ErrorContains(t, internalsdui.Validate(graph("robot")), "prop nodes: [0]: kind")
}
