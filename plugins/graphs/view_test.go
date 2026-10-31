package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/sdui"
	sdk "clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

func TestExecutionView(t *testing.T) {
	r := &Run{
		ID: "gr_1", Title: "Review loop", Status: statusRunning, Turns: 3, MaxTurns: 12, Input: "add a flag\nand tests",
		StartedAt: time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC),
		Nodes: []RunNode{
			{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"},
			{ID: "reviewer", Role: "Reviewer", Session: "agt_2"},
		},
		Edges: []RunEdge{
			{From: "coder", To: "reviewer", On: onDone},
			{From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes"},
		},
	}
	sessions := map[string]api.Session{
		"agt_1": {ID: "agt_1", Title: "Review loop · Coder", Status: "done"},
		"agt_2": {ID: "agt_2", Title: "Review loop · Reviewer", Status: "waiting"},
	}

	side := sidebarView([]*Run{r}, "gr_1", sessions)
	items, ok := side.Props["items"].([]sdk.Item)
	require.True(t, ok)
	assert.Equal(t, "waiting", items[0].Badge)
	assert.Equal(t, []string{"stop", "remove"}, []string{items[0].Menu[0].Action, items[0].Menu[1].Action})

	view := mainView(r, sessions)
	require.NoError(t, sdui.Validate(view))
	assert.Equal(t, "Input: add a flag…", view.Children[2].Props["text"])
	tree := view.Children[3]
	nodes, ok := tree.Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	require.Len(t, nodes, 2)
	assert.Equal(t, "done", nodes[0].Badge)
	assert.Equal(t, "coder · start · Review loop · Coder", nodes[0].Detail)
	assert.Equal(t, "warning", nodes[1].Tone)
	assert.Equal(t, "on outcome changes", nodes[1].Children[0].Detail)
}
