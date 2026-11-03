package sdui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"

	"clubmate/pkg/sdui"
)

func TestNodeJSONShape(t *testing.T) {
	n := sdui.VStack(sdui.Button("new", "New", ""), sdui.MutedText("hi"))
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"stack","props":{"direction":"vertical"},"children":[
		{"type":"button","id":"new","props":{"label":"New"}},
		{"type":"text","props":{"text":"hi","style":"muted"}}]}`, string(raw))
}

func TestSparkValuesClamped(t *testing.T) {
	values := []float64{-5, 50, 101}
	want := []float64{0, 50, 100}
	assert.Equal(t, want, sdui.Sparkline("s", values, 100).Props["values"])
	assert.Equal(t, want, sdui.NewSpark(values, 100).Values)
}

func TestSettingRowOmitsEmptyHint(t *testing.T) {
	label := func(n sdui.Node) []sdui.Node { return n.Children[0].Children }
	assert.Len(t, label(sdui.SettingRow("A", "", sdui.Toggle("t", true))), 1)
	assert.Len(t, label(sdui.SettingRow("A", "hint", sdui.Toggle("t", true))), 2)
}

func TestGraphNodeValidates(t *testing.T) {
	n := sdui.Graph("run", sdui.GraphProps{
		Nodes: []sdui.GraphNode{
			{ID: "slicer", Label: "Slicer", State: "idle · 3 turns", Tone: "success", Icon: "checkmark.circle", Start: true, Outs: []sdui.GraphOut{
				{ID: "step", Label: "step", Count: 3, State: "last"}, {ID: "complete", Label: "complete", State: "never"},
			}},
			{ID: "quizzer", Label: "Quizzer", State: "waiting for you", Tone: "warning", Attention: true, Outs: []sdui.GraphOut{
				{ID: "understood", Label: "understood", Count: 1, State: "taken"},
			}},
			{ID: "reconciler", Label: "Reconciler", State: "not started", Pending: true, Outs: []sdui.GraphOut{
				{ID: "reported", Label: "reported", State: "never", End: true},
			}},
		},
		Edges: []sdui.GraphEdge{
			{From: "slicer", Out: "step", To: "quizzer", State: "last"},
			{From: "quizzer", Out: "understood", To: "slicer", State: "taken"},
			{From: "slicer", Out: "complete", To: "reconciler", State: "never"},
		},
		Moves: []sdui.GraphMove{
			{Node: "slicer", Cause: -1, Tooltip: "move 1 · Slicer · 23:20"},
			{Node: "quizzer", Cause: 0, Out: "step", Tooltip: "move 2 · Quizzer · 23:22", Current: true, Last: true},
		},
	})
	assert.NoError(t, internalsdui.Validate(n))
}
