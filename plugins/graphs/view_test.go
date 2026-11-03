package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
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
			{From: "coder", To: "reviewer", On: onDone, Trigger: "trg_1"},
			{From: "coder", To: "reviewer", On: onError, Trigger: "trg_2"},
			{From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes", Trigger: "trg_3"},
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
	g := view.Children[2]
	assert.Equal(t, "gr_1/graph", g.ID)
	assert.NotContains(t, g.Props, "moves")
	nodes, ok := g.Props["nodes"].([]sdk.GraphNode)
	require.True(t, ok)
	states := make([]string, 0, len(nodes))
	for _, n := range nodes {
		states = append(states, n.State)
	}
	assert.Equal(t, []string{"idle", "waiting for you", "not started", "closed"}, states)
	assert.Equal(t, []string{"done", "error"}, outIDs(nodes[0]))
	assert.Equal(t, []string{"changes"}, outIDs(nodes[1]))
	assert.Equal(t, []sdk.GraphEdge{
		{From: "coder", Out: "done", To: "reviewer", State: "never", Ref: "trg_1", Tooltip: "when Coder is done → prompt Reviewer"},
		{From: "coder", Out: "error", To: "reviewer", State: "never", Ref: "trg_2", Tooltip: "when Coder fails → prompt Reviewer"},
		{From: "reviewer", Out: "changes", To: "coder", State: "never", Ref: "trg_3", Tooltip: "when Reviewer reports changes → prompt Coder"},
	}, g.Props["edges"])

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
	diagram := view.Children[1]
	assert.Equal(t, "graph:pr-walkthrough/graph", diagram.ID)
	assert.NotContains(t, diagram.Props, "moves")
	nodes, ok := diagram.Props["nodes"].([]sdk.GraphNode)
	require.True(t, ok)
	assert.Equal(t, sdk.GraphNode{ID: "slicer", Label: "Slicer", Start: true, Outs: []sdk.GraphOut{{ID: "step", Label: "step", State: "plain"}, {ID: "complete", Label: "complete", State: "plain"}}}, nodes[0])
	assert.Equal(t, []sdk.GraphOut{{ID: "reported", Label: "reported", State: "plain", End: true}}, nodes[2].Outs)
	edges, ok := diagram.Props["edges"].([]sdk.GraphEdge)
	require.True(t, ok)
	assert.Equal(t, sdk.GraphEdge{From: "quizzer", Out: "finish", To: "reconciler", State: "plain", Tooltip: "when Quizzer reports finish → prompt Reconciler"}, edges[len(edges)-1])

	invalid := graphView(graphFile{Name: "broken", Error: "invalid graph JSON"}, nil, draft{}, "")
	assert.Empty(t, invalid.Children[0].Children[0].Children)
}

func TestStoppedRunShowsItsPath(t *testing.T) {
	at := time.Date(2026, 10, 31, 3, 7, 0, 0, time.UTC)
	spec := &Graph{
		Nodes: []Node{{ID: "coder", Start: true}, {ID: "reviewer", Outcomes: []string{"changes", "approved"}, End: []string{"approved"}}},
		Edges: []Edge{{From: "coder", To: "reviewer", On: onDone}, {From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes"}},
	}
	r := &Run{
		ID: "gr_1", Title: "Review loop", Status: statusStopped, Spec: spec,
		Nodes: []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}, {ID: "reviewer", Role: "Reviewer", Session: "agt_2"}},
		Edges: []RunEdge{{From: "coder", To: "reviewer", On: onDone, Trigger: "trg_1"}, {From: "reviewer", To: "coder", On: onOutcome, Outcome: "changes", Trigger: "trg_2"}},
		Moves: []Move{
			{Node: "coder", Cause: -1, Edge: -1, Fired: []int{0}, At: at, Turn: 1},
			{Node: "reviewer", Cause: 0, Edge: 0, Fired: []int{1}, At: at, Turn: 1},
			{Node: "coder", Cause: 1, Edge: 1, Fired: []int{0}, At: at, Turn: 2},
			{Node: "coder", Cause: -1, Edge: -1, User: true, At: at, Turn: 3},
			{Node: "reviewer", Cause: 2, Edge: 0, End: "approved", At: at, Turn: 2, Open: true},
		},
	}
	sessions := map[string]api.Session{"agt_1": {ID: "agt_1", Status: "done"}, "agt_2": {ID: "agt_2", Status: "done"}}

	g := runDiagram(r, sessions)

	require.NoError(t, sdui.Validate(g))
	assert.Equal(t, true, g.Props["reached"])
	nodes, ok := g.Props["nodes"].([]sdk.GraphNode)
	require.True(t, ok)
	assert.Equal(t, []string{"idle · 3 turns", "approved"}, []string{nodes[0].State, nodes[1].State})
	assert.Equal(t, []sdk.GraphOut{{ID: "changes", Label: "changes", Count: 1, State: "taken"}, {ID: "approved", Label: "approved", Count: 1, State: "last", End: true}}, nodes[1].Outs)
	assert.Equal(t, []sdk.GraphEdge{
		{From: "coder", Out: "done", To: "reviewer", State: "last", Tooltip: "when Coder is done → prompt Reviewer"},
		{From: "reviewer", Out: "changes", To: "coder", State: "taken", Tooltip: "when Reviewer reports changes → prompt Coder"},
	}, g.Props["edges"])
	hhmm := at.Local().Format("15:04")
	assert.Equal(t, []sdk.GraphMove{
		{Node: "coder", Cause: -1, Tooltip: "move 1 · Coder · " + hhmm},
		{Node: "reviewer", Cause: 0, Out: "done", Tooltip: "move 2 · Reviewer · " + hhmm},
		{Node: "coder", Cause: 1, Out: "changes", Tooltip: "move 3 · Coder · " + hhmm},
		{Node: "coder", Cause: -1, Label: "you", User: true, Tooltip: "move 4 · Coder · " + hhmm},
		{Node: "reviewer", Cause: 2, Out: "done", Last: true, End: "approved", Tooltip: "move 5 · Reviewer · " + hhmm},
	}, g.Props["moves"])
}

func TestOpenMoveRevealsTurn(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.runs["gr_1"] = &Run{
		ID: "gr_1", Workspace: "ws1", Status: statusRunning,
		Nodes: []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}, {ID: "reviewer", Role: "Reviewer", Session: "agt_2"}},
		Moves: []Move{{Node: "coder", Cause: -1, Edge: -1, Turn: 1}, {Node: "reviewer", Cause: 0, Edge: 0, Turn: 3}},
	}
	var got []any
	a.revealTurn = func(_ context.Context, workspace, session string, turn int) error {
		got = append(got, workspace, session, turn)
		return nil
	}

	_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "gr_1/graph", Action: "openMove", Value: json.RawMessage("1")})

	require.NoError(t, err)
	_, err = a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "gr_1/graph", Action: actOpen, Value: json.RawMessage(`"coder"`)})
	require.NoError(t, err)
	assert.Equal(t, []any{"ws1", "agt_2", 3, "ws1", "agt_1", 0}, got)
}

func TestOpenEdgeSelectsItsTrigger(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.runs["gr_1"] = &Run{ID: "gr_1", Workspace: "ws1", Status: statusRunning, Edges: []RunEdge{{From: "coder", To: "reviewer", On: onDone, Trigger: "trg_1"}}}
	var got []string
	a.selectTrig = func(_ context.Context, workspace, trigger string) error {
		got = append(got, workspace, trigger)
		return nil
	}
	edge := func(trigger string) error {
		_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "gr_1/graph", Action: actOpenEdge, Value: json.RawMessage(`"` + trigger + `"`)})
		return err
	}

	require.NoError(t, edge("trg_1"))
	assert.ErrorContains(t, edge("trg_other"), `graph run "gr_1" has no trigger "trg_other"`)
	a.runs["gr_1"].Status = statusStopped
	assert.ErrorContains(t, edge("trg_1"), `graph run "gr_1" is stopped: its triggers are gone`)
	assert.Equal(t, []string{"ws1", "trg_1"}, got)
}

func TestOpenOnGraphDefinitionIsIgnored(t *testing.T) {
	a := testApp(t, &fakeCore{})
	a.revealTurn = func(context.Context, string, string, int) error {
		t.Fatal("revealed a session")
		return nil
	}

	_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "graph:pr-walkthrough/graph", Action: actOpen, Value: json.RawMessage(`"slicer"`)})

	require.NoError(t, err)
}

func outIDs(n sdk.GraphNode) []string {
	ids := make([]string, 0, len(n.Outs))
	for _, o := range n.Outs {
		ids = append(ids, o.ID)
	}
	return ids
}
