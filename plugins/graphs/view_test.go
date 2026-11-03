package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/sdui"
	sdk "clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/shortcuttest"
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

	side := sidebarView(nil, []*Run{r}, "gr_1", sessions, nil)
	groups, ok := side.Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	items := groups[0].Children
	assert.Equal(t, []string{"exclamationmark.circle", "warning"}, []string{items[0].Icon, items[0].Tone})
	assert.Empty(t, items[0].Badge)
	assert.Equal(t, []sdk.MenuEntry{{Action: actStop, Label: "Stop", Confirm: stopConfirm(r)}}, items[0].Menu)

	view := mainView(r, sessions)
	require.NoError(t, sdui.Validate(view))
	header := view.Children[0]
	assert.Equal(t, []sdk.HeaderBadge{{Text: "running · turn 3 of 12"}, {Text: "Reviewer needs you", Tone: "warning"}}, header.Props["badges"])
	assert.Equal(t, "Open Reviewer", header.Children[0].Props["label"])
	assert.Equal(t, []string{"gr_1/open", "gr_1/stop"}, []string{header.Children[0].ID, header.Children[1].ID})
	require.Len(t, header.Children, 2)
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

func TestStoppedRunOffersRemove(t *testing.T) {
	r := &Run{ID: "gr_1", Title: "Review loop", Status: statusStopped, StartedAt: time.Now()}

	groups, ok := sidebarView(nil, []*Run{r}, "gr_1", nil, nil).Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	items := groups[0].Children
	assert.Equal(t, []string{actRemove}, []string{items[0].Menu[0].Action})
	assert.Equal(t, "", items[0].Tone)
	header := mainView(r, nil).Children[0]
	remove := header.Children[len(header.Children)-1]
	assert.Equal(t, "gr_1/remove", remove.ID)
	assert.Equal(t, true, remove.Props["overflow"])
}

func TestViewShortcutsDeclared(t *testing.T) {
	running := &Run{ID: "gr_1", Title: "Review loop", Status: statusRunning, Nodes: []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}}}
	stopped := &Run{ID: "gr_2", Title: "Review loop", Status: statusStopped}
	shortcuttest.RequireDeclared(t, mainView(running, map[string]api.Session{"agt_1": {ID: "agt_1", Status: "waiting"}}), mainView(stopped, nil), graphView(graphFile{Name: "review-loop"}, nil, draft{}, ""))
}

func TestBuiltinRunDefinition(t *testing.T) {
	r := &Run{ID: "gr_1", Graph: "pr-walkthrough", Title: "PR walkthrough", Status: statusRunning, Builtin: true, StartedAt: time.Now()}

	props := mainView(r, nil).Children[1].Children
	assert.Equal(t, automation.Property("Definition", "Built into Clubmate", ""), props[len(props)-1])
}

func TestSidebarListsGraphsAboveRuns(t *testing.T) {
	graphs := []graphFile{
		{Name: "broken", Error: "nodes: at least one node is required"},
		{Name: "pr-walkthrough", Title: "PR walkthrough", Nodes: 4, Builtin: true},
		{Name: "review-loop", Title: "Review loop", Nodes: 2},
	}
	runs := []*Run{{ID: "gr_1", Title: "Review loop", Status: statusStopped, StartedAt: time.Now()}}

	groups, ok := sidebarView(graphs, runs, "gr_1", nil, map[string]bool{groupRuns: true}).Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	assert.Equal(t, []string{"Graphs", "3", "Runs", "1"}, []string{groups[0].Label, groups[0].Detail, groups[1].Label, groups[1].Detail})
	assert.Equal(t, []bool{true, false}, []bool{groups[0].Expanded, groups[1].Expanded})
	items := groups[0].Children
	assert.Equal(t, []string{"graph:broken", "invalid", "danger"}, []string{items[0].ID, items[0].Detail, items[0].Tone})
	assert.Equal(t, []string{"PR walkthrough", "built in"}, []string{items[1].Label, items[1].Detail})
	assert.Equal(t, "2 nodes", items[2].Detail)

	assert.Equal(t, "graph:pr-walkthrough", pickSelected("", graphs, nil))
	assert.Equal(t, "gr_1", pickSelected("graph:gone", graphs, runs))
}

func TestGraphViewRunsWithInput(t *testing.T) {
	spec, builtin, err := loadGraph(nil, "pr-walkthrough")
	require.NoError(t, err)
	g := graphFile{Name: "pr-walkthrough", Title: spec.Title, Description: spec.Description, Builtin: builtin}

	view := graphView(g, spec, draft{graph: "pr-walkthrough", input: "{{x}}", err: "Input: must not contain {{."}, "pr-walkthrough")
	require.NoError(t, sdui.Validate(view))
	form := view.Children[0]
	assert.Equal(t, []any{"graph:pr-walkthrough/run", actRun}, []any{form.ID, form.Props["action"]})
	run := form.Children[0].Children[0]
	assert.Equal(t, []any{"graph:pr-walkthrough/submit", "cmd+return", "Starting…"}, []any{run.ID, run.Props["shortcut"], run.Props["loading"]})
	assert.Contains(t, form.Children, sdk.HStack(sdk.MutedText("Input"), sdk.TextField(fieldInput, "{{x}}", "What the run works on; ↩ runs it")))
	assert.Contains(t, form.Children, sdk.HStack(sdk.MutedText(""), sdk.WithTone(sdk.Text("Input: must not contain {{."), "danger")))
	assert.Contains(t, form.Children, automation.Property("Definition", "Built into Clubmate", ""))
	rows, ok := view.Children[1].Children[0].Props["rows"].([]sdk.TableRow)
	require.True(t, ok)
	assert.Equal(t, []string{"Slicer · start", ""}, cellTexts(rows[0]))

	invalid := graphView(graphFile{Name: "broken", Error: "invalid graph JSON"}, nil, draft{}, "")
	assert.Empty(t, invalid.Children[0].Children[0].Children)
}

func cellTexts(row sdk.TableRow) []string {
	texts := make([]string, 0, len(row.Cells))
	for _, c := range row.Cells {
		texts = append(texts, c.Text)
	}
	return texts
}
