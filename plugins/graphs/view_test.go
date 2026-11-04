package main

import (
	"context"
	"encoding/json"
	"strings"
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

	side := sidebarView(nil, nil, []*Run{r}, "gr_1", sessions, nil)
	groups, ok := side.Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	items := groups[0].Children
	assert.Equal(t, []string{"exclamationmark.circle", "warning"}, []string{items[0].Icon, items[0].Tone})
	assert.Empty(t, items[0].Badge)
	assert.Equal(t, []sdk.MenuEntry{{Action: actStop, Label: "Stop", Confirm: stopConfirm(r)}}, items[0].Menu)

	view := mainView(r, sessions, runExtras{})
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

	assert.Equal(t, "empty", mainView(nil, nil, runExtras{}).Type)
}

func TestStoppedRunOffersRemove(t *testing.T) {
	r := &Run{ID: "gr_1", Title: "Review loop", Status: statusStopped, StartedAt: time.Now()}

	groups, ok := sidebarView(nil, nil, []*Run{r}, "gr_1", nil, nil).Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	items := groups[0].Children
	assert.Equal(t, []string{actRemove}, []string{items[0].Menu[0].Action})
	assert.Equal(t, "", items[0].Tone)
	header := mainView(r, nil, runExtras{}).Children[0]
	remove := header.Children[len(header.Children)-1]
	assert.Equal(t, "gr_1/remove", remove.ID)
	assert.Equal(t, true, remove.Props["overflow"])
}

func TestViewShortcutsDeclared(t *testing.T) {
	running := &Run{ID: "gr_1", Title: "Review loop", Status: statusRunning, Nodes: []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}}}
	stopped := &Run{ID: "gr_2", Title: "Review loop", Status: statusStopped}
	shortcuttest.RequireDeclared(t, mainView(running, map[string]api.Session{"agt_1": {ID: "agt_1", Status: "waiting"}}, runExtras{}), mainView(stopped, nil, runExtras{}), graphView(graphFile{Name: "review-loop"}, nil, draft{}, "", trustView{}))
}

func TestBuiltinRunDefinition(t *testing.T) {
	r := &Run{ID: "gr_1", Graph: "pr-walkthrough", Title: "PR walkthrough", Status: statusRunning, Builtin: true, StartedAt: time.Now()}

	props := mainView(r, nil, runExtras{}).Children[1].Children
	assert.Equal(t, automation.Property("Definition", "Built into Clubmate", ""), props[len(props)-1])
}

func TestSidebarListsGraphsAboveRuns(t *testing.T) {
	graphs := []graphFile{
		{Name: "broken", Error: "nodes: at least one node is required"},
		{Name: "pr-walkthrough", Title: "PR walkthrough", Nodes: 4, Builtin: true},
		{Name: "review-loop", Title: "Review loop", Nodes: 2},
	}
	runs := []*Run{{ID: "gr_1", Title: "Review loop", Status: statusStopped, StartedAt: time.Now()}}

	groups, ok := sidebarView(graphs, nil, runs, "gr_1", nil, map[string]bool{groupRuns: true}).Props["items"].([]sdk.TreeItem)
	require.True(t, ok)
	assert.Equal(t, []string{"Graphs", "3", "Runs", "1"}, []string{groups[0].Label, groups[0].Detail, groups[1].Label, groups[1].Detail})
	assert.Equal(t, []bool{true, false}, []bool{groups[0].Expanded, groups[1].Expanded})
	assert.Equal(t, []bool{true, true}, []bool{groups[0].Action, groups[1].Action})
	items := groups[0].Children
	assert.Equal(t, []string{"graph:broken", "invalid", "danger"}, []string{items[0].ID, items[0].Detail, items[0].Tone})
	assert.Equal(t, []string{"PR walkthrough", "built in"}, []string{items[1].Label, items[1].Detail})
	assert.Equal(t, "2 nodes", items[2].Detail)

	assert.Equal(t, "graph:pr-walkthrough", pickSelected("", graphs, nil))
	assert.Equal(t, "gr_1", pickSelected("graph:gone", graphs, runs))
}

func TestGraphViewRunsWithInput(t *testing.T) {
	spec, from, err := loadGraph(nil, "", "pr-walkthrough")
	require.NoError(t, err)
	g := graphFile{Name: "pr-walkthrough", Title: spec.Title, Description: spec.Description, Builtin: from == fromBuiltin}

	view := graphView(g, spec, draft{graph: "pr-walkthrough", input: "{{x}}", err: "Input: must not contain {{."}, "pr-walkthrough", trustView{})
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
	assert.Equal(t, sdk.GraphNode{ID: "slicer", Label: "Slicer", Start: true, Kind: sdk.KindAgent, Outs: []sdk.GraphOut{{ID: "step", Label: "step", State: "plain"}, {ID: "complete", Label: "complete", State: "plain"}}}, nodes[0])
	assert.Equal(t, []sdk.GraphOut{{ID: "reported", Label: "reported", State: "plain", End: true}}, nodes[2].Outs)
	edges, ok := diagram.Props["edges"].([]sdk.GraphEdge)
	require.True(t, ok)
	assert.Equal(t, sdk.GraphEdge{From: "quizzer", Out: "finish", To: "reconciler", State: "plain", Tooltip: "when Quizzer reports finish → prompt Reconciler"}, edges[len(edges)-1])

	invalid := graphView(graphFile{Name: "broken", Error: "invalid graph JSON"}, nil, draft{}, "", trustView{})
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
		{Node: "coder", Cause: -1, Tooltip: "move 1 · Coder · " + hhmm, Round: 1},
		{Node: "reviewer", Cause: 0, Out: "done", Tooltip: "move 2 · Reviewer · " + hhmm, Round: 1},
		{Node: "coder", Cause: 1, Out: "changes", Tooltip: "move 3 · Coder · " + hhmm, Round: 2},
		{Node: "coder", Cause: -1, Label: "you", User: true, Tooltip: "move 4 · Coder · " + hhmm, Round: 3},
		{Node: "reviewer", Cause: 2, Out: "done", Last: true, End: "approved", Tooltip: "move 5 · Reviewer · " + hhmm, Round: 3},
	}, g.Props["moves"])
}

func TestFinishedTurnPreviewsBeforeOpening(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := &Run{
		ID: "gr_1", Workspace: "ws1", Status: statusRunning,
		Nodes: []RunNode{{ID: "coder", Role: "Coder", Start: true, Session: "agt_1"}, {ID: "reviewer", Role: "Reviewer", Session: "agt_2"}, {ID: "fixer", Role: "Fixer", Session: "agt_3"}},
		Moves: []Move{{Node: "coder", Cause: -1, Edge: -1, Turn: 1, Summary: "Fixed the flaky test."}, {Node: "reviewer", Cause: 0, Edge: 0, Turn: 3}, {Node: "fixer", Cause: 1, Edge: 1, Turn: 2}},
	}
	a.runs["gr_1"] = r
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_3", Status: "running"}}, nil
	}
	var got []any
	a.revealTurn = func(_ context.Context, workspace, session string, turn int) error {
		got = append(got, workspace, session, turn)
		return nil
	}
	event := func(action, value string) {
		t.Helper()
		_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "gr_1/graph", Action: action, Value: json.RawMessage(value)})
		require.NoError(t, err)
	}

	event(actOpenMove, "1")
	event(actOpenMove, "1")
	event(actOpen, `"coder"`)
	panel := agentPanel(r, "coder", 0, false)
	_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: "gr_1/" + actOpenTurn, Action: actOpenTurn})
	require.NoError(t, err)
	event(actOpen, `"fixer"`)

	assert.Equal(t, []any{"ws1", "agt_2", 3, "ws1", "agt_1", 1, "ws1", "agt_3", 0}, got)
	assert.Equal(t, "Fixed the flaky test.", panel.Children[1].Props["text"])
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

func diagramNodes(t *testing.T, a *app, id string) map[string]sdk.GraphNode {
	t.Helper()
	a.mu.Lock()
	r := cloneRun(a.runs[id])
	a.mu.Unlock()
	tree, err := json.Marshal(runDiagram(r, nil))
	require.NoError(t, err)
	var props struct {
		Props sdk.GraphProps `json:"props"`
	}
	require.NoError(t, json.Unmarshal(tree, &props))
	out := map[string]sdk.GraphNode{}
	for _, n := range props.Props.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestStepCardRendersWithoutSession(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 1, Duration: 64 * time.Second})
	nodes := diagramNodes(t, a, r.ID)
	assert.Equal(t, "not started", nodes["tests"].State)
	turnDone(t, a, "agt_1", 1)
	nodes = diagramNodes(t, a, r.ID)
	assert.Equal(t, []string{"pass", "fail", "flaky", "error"}, outIDs(nodes["tests"]))
	assert.Equal(t, []string{"done", "error"}, outIDs(nodes["merge"]))
	assert.Equal(t, "fail · 1 min 04 s", nodes["tests"].State)
	assert.Equal(t, "xmark.circle", nodes["tests"].Icon)
	assert.Equal(t, "neutral", nodes["tests"].Tone)
	assert.Equal(t, "make test", nodes["tests"].Tooltip)
}

func showNodes(t *testing.T, a *app, r *Run) map[string]shownNode {
	t.Helper()
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.show", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	nodes := map[string]shownNode{}
	for _, n := range out.(shownRun).Nodes {
		nodes[n.ID] = n
	}
	return nodes
}

func TestStepSec_A1_ShowCarriesAudit(t *testing.T) {
	a, _, p, r := approvalApp(t, "auto", "Bash(make test)")
	p.results = []stepResult{{ExitCode: 1, Tail: "FAIL"}}
	turnDone(t, a, "agt_1", 1)
	st := showNodes(t, a, r)["tests"]
	assert.Equal(t, outFail, st.Status)
	require.NotNil(t, st.Last)
	assert.Equal(t, "make test", st.Last.Command)
	assert.Equal(t, "/wt/coder", st.Last.Dir)
	assert.Equal(t, "rule Bash(make test)", st.Last.AllowedBy)
	require.NotNil(t, st.Last.ExitCode)
	assert.Equal(t, 1, *st.Last.ExitCode)
	assert.Equal(t, "FAIL", st.Last.Tail)
}

func TestStepSec_A2_RejectedIsAMove(t *testing.T) {
	a, _, _, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, stepWaiting, showNodes(t, a, r)["tests"].Status)
	require.NoError(t, approve(t, a, r, "tests", "reject"))
	st := showNodes(t, a, r)["tests"]
	assert.Equal(t, onError, st.Status)
	assert.Equal(t, reasonRejected, st.Last.Reason)
	assert.Equal(t, "error · rejected", diagramNodes(t, a, r.ID)["tests"].State)
}

func TestOpenStepCardDoesNothing(t *testing.T) {
	a, _, _, r := stepApp(t)
	revealed := false
	a.revealTurn = func(context.Context, string, string, int) error {
		revealed = true
		return nil
	}
	assert.NoError(t, a.openGraph(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws1", Action: actOpen, Value: json.RawMessage(`"tests"`)}, r.ID))
	assert.False(t, revealed)
}

func runView(t *testing.T, a *app, id string) sdk.Node {
	t.Helper()
	a.mu.Lock()
	r := cloneRun(a.runs[id])
	a.mu.Unlock()
	view := mainView(r, nil, a.extras(r))
	require.NoError(t, sdui.Validate(view))
	return view
}

func graphChildren(view sdk.Node) []sdk.Node {
	return view.Children[2].Children
}

func TestStepCardProps(t *testing.T) {
	a, _, p, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	nodes := diagramNodes(t, a, r.ID)
	assert.Equal(t, []string{sdk.KindAgent, sdk.KindCheck, sdk.KindAction}, []string{nodes["coder"].Kind, nodes["tests"].Kind, nodes["merge"].Kind})
	assert.Equal(t, []string{"make test", "git merge --squash x"}, []string{nodes["tests"].Command, nodes["merge"].Command})
	assert.Equal(t, "2026-10-31T03:00:00Z", nodes["tests"].Since)
	assert.Zero(t, nodes["tests"].Timeout)
	p.block = true
	go func() { _ = approve(t, a, r, "tests", "once") }()
	require.Eventually(t, func() bool { return diagramNodes(t, a, r.ID)["tests"].Timeout == 600 }, 5*time.Second, 10*time.Millisecond)
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
}

func TestApprovalCard(t *testing.T) {
	a, _, _, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	view := runView(t, a, r.ID)
	assert.Equal(t, []sdk.HeaderBadge{{Text: "waiting for you", Tone: "warning"}}, view.Children[0].Props["badges"])
	require.Len(t, view.Children[0].Children, 1)
	card := graphChildren(view)[0].Children[0]
	assert.Equal(t, "permissionCard", card.Type)
	assert.Equal(t, []any{r.ID + "/approve", "tests", "Run a command?", "make test", "Tests · a step of Review gate", "approve"}, []any{card.ID, card.Props["request"], card.Props["title"], card.Props["command"], card.Props["caption"], card.Props["action"]})
	assert.Equal(t, []sdk.PermissionOption{
		{ID: "once", Label: "Allow once", Kind: "allow_once", Shortcut: "opt+1"},
		{ID: "always", Label: "Always allow…", Kind: "allow_always", Shortcut: "opt+2", Rule: "Bash(make test)", Scope: "in the main checkout (/repo)"},
		{ID: "reject", Label: "Reject", Kind: "reject_once", Shortcut: "opt+3"},
	}, card.Props["options"])
	require.NoError(t, a.approveFromSection(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws1", Action: actApprove, Value: json.RawMessage(`{"request":"tests","option":"reject"}`)}, r.ID))
	assert.Equal(t, reasonRejected, stepMoves(a, r.ID, "tests")[0].Step.Reason)
}

func TestHeldEdgeView(t *testing.T) {
	a, core, _, r := stepApp(t, stepResult{ExitCode: 1, Tail: "FAIL a"}, stepResult{ExitCode: 1, Tail: "FAIL b"}, stepResult{ExitCode: 1, Tail: "one\ntwo\n"})
	for turn := 1; turn <= 3; turn++ {
		turnDone(t, a, "agt_1", turn)
	}
	view := runView(t, a, r.ID)
	header := view.Children[0]
	assert.Equal(t, sdk.HeaderBadge{Text: "waiting for you · round cap", Tone: "warning"}, header.Props["badges"].([]sdk.HeaderBadge)[0])
	row := sidebarView(nil, nil, []*Run{a.runs[r.ID]}, r.ID, nil, nil).Props["items"].([]sdk.TreeItem)[0].Children[0]
	assert.Equal(t, []string{"2 of 2", "exclamationmark.circle", "warning"}, []string{row.Detail, row.Icon, row.Tone})
	assert.Equal(t, []string{r.ID + "/continue", "Continue", "cmd+return"}, []string{header.Children[0].ID, header.Children[0].Props["label"].(string), header.Children[0].Props["shortcut"].(string)})
	edges := view.Children[2].Props["edges"].([]sdk.GraphEdge)
	assert.Equal(t, []string{sdk.EdgeHeld, "×2/2 · waits for you"}, []string{edges[1].State, edges[1].Label})
	children := graphChildren(view)
	require.Len(t, children, 2)
	assert.Equal(t, []any{"card", "warning"}, []any{children[0].Type, children[0].Props["tone"]})
	assert.Equal(t, "Tests → Coder reached its limit of 2 rounds. Continue allows one more round; Stop run ends the run.", texts(children[0])[0])
	assert.Equal(t, []string{"Tests", "round 3 · fail · exit 1 · 0 s", "one\ntwo"}, texts(children[1])[:3])
	require.NoError(t, a.continueFromSection(t.Context(), "ws1", r.ID))
	assert.Len(t, prompts(t, core, "agt_1"), 4)
}

func TestOutputPanelWhenEmpty(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 0})
	turnDone(t, a, "agt_1", 1)
	panel := graphChildren(runView(t, a, r.ID))[0]
	assert.Equal(t, []string{"Tests", "pass · exit 0 · 0 s", "No output — the command printed nothing.\nmake test in /wt/coder"}, texts(panel))
}

func TestOutputPanelCountsWholeOutput(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 1, Tail: strings.Repeat("line\n", 30) + "end", Lines: 212})
	turnDone(t, a, "agt_1", 1)
	panel := graphChildren(runView(t, a, r.ID))[0]
	assert.Contains(t, texts(panel), "last 12 of 212 lines")
}

func TestOutputPanelFlagsCheckFiles(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 0, CheckFiles: []string{"Makefile", "rules.mk"}})
	turnDone(t, a, "agt_1", 1)
	panel := graphChildren(runView(t, a, r.ID))[0]
	assert.Equal(t, []any{"The check's own files changed in this run: Makefile, rules.mk.", "warning"}, []any{panel.Children[1].Props["text"], panel.Children[1].Props["tone"]})
}

func texts(n sdk.Node) []string {
	var out []string
	if n.Type == "text" {
		out = append(out, n.Props["text"].(string))
	}
	for _, c := range n.Children {
		out = append(out, texts(c)...)
	}
	return out
}

func TestHistoryDrawsStepBarsAndRounds(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 1, Tail: "FAIL", Duration: 64 * time.Second})
	turnDone(t, a, "agt_1", 1)
	moves := runView(t, a, r.ID).Children[2].Props["moves"].([]sdk.GraphMove)
	require.Len(t, moves, 3)
	assert.Equal(t, []string{"", "fail · 1:04", ""}, []string{moves[0].Bar, moves[1].Bar, moves[2].Bar})
	assert.Equal(t, "neutral", moves[1].Tone)
	assert.Equal(t, []int{1, 1, 2}, []int{moves[0].Round, moves[1].Round, moves[2].Round})
}
