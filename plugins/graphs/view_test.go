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
			{ID: "tester", Role: "Tester", Session: "agt_3"},
			{ID: "fixer", Role: "Fixer", Session: "agt_4"},
		},
		Edges: []RunEdge{
			{From: "coder", To: "reviewer", On: onDone},
			{From: "coder", To: "reviewer", On: onError},
			{From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes"},
		},
	}
	sessions := map[string]api.Session{
		"agt_1": {ID: "agt_1", Title: "Review loop · Coder", Status: "done"},
		"agt_2": {ID: "agt_2", Title: "Review loop · Reviewer", Status: "waiting"},
		"agt_3": {ID: "agt_3", Title: "Review loop · Tester", Status: "idle"},
	}

	side := sidebarView([]*Run{r}, "gr_1", sessions)
	items, ok := side.Props["items"].([]sdk.Item)
	require.True(t, ok)
	assert.Equal(t, "waiting", items[0].Badge)
	assert.Equal(t, []string{"stop", "remove"}, []string{items[0].Menu[0].Action, items[0].Menu[1].Action})

	view := mainView(r, sessions)
	require.NoError(t, sdui.Validate(view))
	header := view.Children[0]
	assert.Equal(t, []sdk.HeaderBadge{{Text: "running · turn 3 of 12"}, {Text: "Reviewer needs you", Tone: "warning"}}, header.Props["badges"])
	assert.Equal(t, "Open Reviewer", header.Children[0].Props["label"])
	assert.Equal(t, []string{"gr_1/open", "gr_1/stop", "gr_1/remove"}, []string{header.Children[0].ID, header.Children[1].ID, header.Children[2].ID})
	rows, ok := view.Children[2].Children[0].Props["rows"].([]sdk.TableRow)
	require.True(t, ok)
	require.Len(t, rows, 6)
	assert.Equal(t, []string{"Coder · start", "done", "Review loop · Coder"}, cellTexts(rows[0]))
	assert.Equal(t, []string{"→ Reviewer", "on done · on error", ""}, cellTexts(rows[1]))
	assert.Equal(t, sdk.Cell{Text: "waiting for you", Tone: "warn"}, rows[2].Cells[1])
	assert.Equal(t, []string{"→ Coder", "on outcome changes", ""}, cellTexts(rows[3]))
	assert.Equal(t, []string{"Tester", "idle", "not started"}, cellTexts(rows[4]))
	assert.Equal(t, []string{"Fixer", "session closed", ""}, cellTexts(rows[5]))

	assert.Equal(t, "empty", mainView(nil, nil).Type)
}

func cellTexts(row sdk.TableRow) []string {
	texts := make([]string, 0, len(row.Cells))
	for _, c := range row.Cells {
		texts = append(texts, c.Text)
	}
	return texts
}
