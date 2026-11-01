package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	isdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

func TestGraphTriggersGroupUnderTheirRun(t *testing.T) {
	titles := map[string]string{"agt_1": "Review loop · Coder", "agt_2": "Review loop · Reviewer", "gr_1": "Review loop"}
	edge := &Trigger{ID: "tr_1", Name: "Review loop: Coder done → Reviewer", Enabled: true, Run: "gr_1", RunTitle: "Review loop",
		On: "agents.turn", Match: json.RawMessage(`{"session":"agt_1","status":"done"}`),
		Command: automation.PromptCommand, Args: json.RawMessage(`{"session":"agt_2","text":"Review the last commit."}`)}
	mine := &Trigger{ID: "tr_2", Name: "Nightly", Enabled: true, On: "event.nightly", Command: automation.PromptCommand}

	side := sidebarView([]*Trigger{edge, mine}, "tr_1", map[string]bool{"graph:gr_1": true}, titles)
	require.NoError(t, isdui.Validate(side))
	items, ok := side.Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	require.Len(t, items, 2)
	assert.Equal(t, "tr_2", items[0].ID)
	assert.Equal(t, []string{"from graph Review loop", "1"}, []string{items[1].Label, items[1].Detail})
	assert.False(t, items[1].Expanded)
	assert.Equal(t, "Coder done → Reviewer", items[1].Children[0].Label)

	view := mainView(edge, titles, automation.Open{})
	require.NoError(t, isdui.Validate(view))
	header := view.Children[0]
	assert.Equal(t, "created by graph Review loop", header.Props["meta"])
	assert.Equal(t, []string{"tr_1/run", "tr_1/pause", "tr_1/graph", "tr_1/delete"}, childIDs(header))
	assert.Equal(t, true, header.Children[3].Props["overflow"])
	assert.Contains(t, header.Children[3].Props["confirm"], "without it the run stops at this step")
	got := texts(view)
	assert.Contains(t, got, "an agent turn finishes · status done · session Review loop · Coder")
	assert.Contains(t, got, "graph Review loop")
	assert.Contains(t, got, "Send a prompt to Review loop · Reviewer")

	assert.Equal(t, "tr_2/delete", childIDs(mainView(mine, nil, automation.Open{}).Children[0])[2])
}

func texts(n sdui.Node) []string {
	var out []string
	if s, ok := n.Props["text"].(string); ok && n.Type == "text" {
		out = append(out, s)
	}
	for _, c := range n.Children {
		out = append(out, texts(c)...)
	}
	return out
}

func childIDs(n sdui.Node) []string {
	ids := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		ids = append(ids, c.ID)
	}
	return ids
}
