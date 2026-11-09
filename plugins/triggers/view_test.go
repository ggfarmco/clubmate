package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	isdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/shortcuttest"
)

func TestGraphTriggersGroupUnderTheirRun(t *testing.T) {
	titles := map[string]string{"agt_1": "Review loop · Coder", "agt_2": "Review loop · Reviewer", "gr_1": "Review loop"}
	edge := &Trigger{ID: "tr_1", Name: "Review loop: Coder done → Reviewer", Enabled: true, Run: "gr_1", RunTitle: "Review loop",
		On: "agents.turn", Match: json.RawMessage(`{"session":"agt_1","status":"done"}`),
		Command: automation.PromptCommand, Args: json.RawMessage(`{"session":"agt_2","text":"Review the last commit."}`)}
	fails := &Trigger{ID: "tr_3", Name: "Review loop: Coder fails → Reviewer", Enabled: true, Run: "gr_1", RunTitle: "Review loop",
		On: "agents.turn", Match: json.RawMessage(`{"session":"agt_1","status":"error"}`),
		Command: automation.PromptCommand, Args: json.RawMessage(`{"session":"agt_2","text":"Fix it."}`)}
	mine := &Trigger{ID: "tr_2", Name: "Nightly", Enabled: true, On: "event.nightly", Command: automation.PromptCommand}

	side := sidebarView([]*Trigger{edge, mine}, "tr_1", map[string]bool{"graph:gr_1": true}, titles)
	require.NoError(t, isdui.Validate(side))
	items, ok := side.Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	require.Len(t, items, 3)
	assert.Equal(t, []string{automation.HelpID, "tr_2"}, []string{items[0].ID, items[1].ID})
	assert.Equal(t, []string{"from graph Review loop", "1"}, []string{items[2].Label, items[2].Detail})
	assert.False(t, items[2].Expanded)
	assert.True(t, items[2].Action)
	assert.Equal(t, "Coder done → Reviewer", items[2].Children[0].Label)

	shape := &graphShape{
		Nodes: []shapeNode{{ID: "coder", Session: "agt_1", Start: true}, {ID: "reviewer", Session: "agt_2", End: []string{"approved"}}},
		Edges: []shapeEdge{{From: "coder", To: "reviewer", On: "done", Trigger: "tr_1"}, {From: "reviewer", To: "coder", On: "outcome", Outcome: "changes"}},
	}
	view := mainView(edge, []*Trigger{edge, fails, mine}, titles, shape, automation.Open{}, automation.RunsView{})
	require.NoError(t, isdui.Validate(view))
	header := view.Children[0]
	assert.Equal(t, "created by graph Review loop", header.Props["meta"])
	assert.Equal(t, []string{"tr_1/run", "tr_1/pause", "tr_1/graph", "tr_1/delete"}, childIDs(header))
	assert.Equal(t, sdui.VariantSecondary, header.Children[0].Props["variant"])
	assert.Equal(t, true, header.Children[3].Props["overflow"])
	assert.Contains(t, header.Children[3].Props["confirm"], "without it the run stops at this step")
	got := texts(view)
	assert.Contains(t, got, "an agent turn finishes · status done · session Review loop · Coder")
	assert.Contains(t, got, "graph Review loop")
	assert.Contains(t, got, "Send a prompt to Review loop · Reviewer")

	flow := find(view, "flow")
	cards, ok := flow.Props["cards"].([]sdui.FlowCard)
	require.True(t, ok)
	assert.Equal(t, []string{"agt_1", "agt_2"}, []string{cards[0].ID, cards[1].ID})
	assert.Equal(t, []string{"Coder", "Reviewer"}, []string{cards[0].Label, cards[1].Label})
	edges, ok := flow.Props["edges"].([]sdui.FlowEdge)
	require.True(t, ok)
	assert.Equal(t, []sdui.FlowEdge{
		{From: "agt_1", Out: "done", To: "agt_2", State: sdui.EdgeSelected, Tooltip: "this trigger", Label: "this trigger"},
		{From: "agt_1", Out: "error", To: "agt_2", State: sdui.EdgeDim, Ref: "tr_3", Tooltip: "Coder fails → Reviewer"},
		{From: "agt_2", Out: "changes", To: "agt_1", State: sdui.EdgeNever, Tooltip: "changes → Coder · the graph passes it on itself"},
		{From: "start:coder", To: "agt_1", State: sdui.EdgeDim},
		{From: "agt_2", Out: "approved", To: "end", State: sdui.EdgeNever, Tooltip: "approved ends the run"},
	}, edges)
	assert.Equal(t, []sdui.FlowMarker{{ID: "start:coder", Kind: sdui.MarkerStart}, {ID: "end", Kind: sdui.MarkerEnd}}, flow.Props["markers"])

	own := mainView(mine, []*Trigger{mine}, nil, nil, automation.Open{}, automation.RunsView{}).Children[0]
	assert.Equal(t, "tr_2/delete", childIDs(own)[2])
	assert.Equal(t, sdui.VariantPrimary, own.Children[0].Props["variant"])
	shortcuttest.RequireDeclared(t, view)
}

func TestUserTriggerDrawsItsLastChainAndRunsAsLanes(t *testing.T) {
	at := time.Date(2026, 11, 3, 2, 36, 0, 0, time.UTC)
	chain := &automation.Chain{Session: "agt_9", Title: "Nightly tidy", Status: "done", CronJob: "cj_1", CronName: "Nightly tidy"}
	release := &Trigger{ID: "tr_5", Name: "Release after the nightly tidy", Enabled: true, On: "event.tidy.done",
		Command: "agents.new", Args: json.RawMessage(`{"prompt":"Release"}`), History: []Run{
			{At: at.Add(-24 * time.Hour), Topic: "event.tidy.done", Status: automation.StatusOK, Result: `{"session":"agt_8"}`, Chain: chain},
			{At: at, Topic: "event.tidy.done", Status: automation.StatusError, Reason: "CI is red on main", Chain: chain},
		}}

	view := mainView(release, []*Trigger{release}, map[string]string{"agt_9": "Nightly tidy"}, nil, automation.Open{}, automation.RunsView{})
	require.NoError(t, isdui.Validate(view))
	assert.Contains(t, texts(view), "last time · cron Nightly tidy started the session that emitted it")
	flow := find(view, "flow")
	assert.Equal(t, []sdui.FlowMarker{{ID: "cron", Kind: sdui.MarkerSchedule, Label: "Nightly tidy"}}, flow.Props["markers"])
	cards, ok := flow.Props["cards"].([]sdui.FlowCard)
	require.True(t, ok)
	require.Len(t, cards, 2)
	assert.Equal(t, []string{"agt_9", "emits tidy.done"}, []string{cards[0].ID, cards[0].Outs[0].Label})
	assert.Equal(t, []string{"effect", "danger"}, []string{cards[1].ID, cards[1].Tone})

	lanes := find(view, "lanes")
	moves, ok := lanes.Props["moves"].([]sdui.LaneMove)
	require.True(t, ok)
	require.Len(t, moves, 4)
	assert.Equal(t, []int{-1, 0, -1, 2}, []int{moves[0].Cause, moves[1].Cause, moves[2].Cause, moves[3].Cause})
	assert.Equal(t, "tidy.done → start", moves[3].Label)
	assert.Equal(t, []string{"success", "danger"}, []string{moves[1].Tone, moves[3].Tone})
	assert.True(t, moves[3].Last)
	assert.Equal(t, automation.RunID(release.History[1]), moves[3].Ref)
	rows := find(view, "table").Props["rows"].([]sdui.TableRow)
	assert.Equal(t, []string{actOpenSession, ""}, []string{rows[0].Cells[3].Action, rows[1].Cells[3].Action})
}

func TestNoTriggersShowGhostFlow(t *testing.T) {
	view := mainView(nil, nil, nil, nil, automation.Open{}, automation.RunsView{})
	require.NoError(t, isdui.Validate(view))
	cards, ok := find(view, "flow").Props["cards"].([]sdui.FlowCard)
	require.True(t, ok)
	assert.True(t, cards[0].Ghost && cards[1].Ghost)
}

func find(n sdui.Node, kind string) sdui.Node {
	if n.Type == kind {
		return n
	}
	for _, c := range n.Children {
		if f := find(c, kind); f.Type != "" {
			return f
		}
	}
	return sdui.Node{}
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

func TestPaceLineShowsOnlyWhatIsSet(t *testing.T) {
	assert.Equal(t, "up to 60 runs a minute · runs overlap", paceLine(&Trigger{Limit: 60, Parallel: true}))
	assert.Equal(t, "runs overlap", paceLine(&Trigger{Parallel: true}))
	assert.Empty(t, paceLine(&Trigger{}))
}

func TestOwnerLineNamesTheAddingPlugin(t *testing.T) {
	assert.Equal(t, "added by remote", ownerLine(&Trigger{CreatedBy: "plugin:remote"}, nil))
}

func TestCauseCardNamesTheEventNotTheLastSession(t *testing.T) {
	at := time.Date(2026, 11, 4, 11, 1, 0, 0, time.UTC)
	chain := &automation.Chain{Session: "agt_1", Title: "Coder", Kind: "done", Status: "done"}
	tests := []struct {
		name  string
		on    string
		match string
		label string
		icon  string
		out   string
	}{
		{"notification of one kind", "notify.posted", `{"kind":"done"}`, "notifications", "bell", "posts done"},
		{"notification of several kinds", "notify.posted", `{"kind":["done","stuck"]}`, "notifications", "bell", "posted"},
		{"any session's turn", "agents.turn", `{"status":"done"}`, "agent turns", "sparkle", "turn ends · done"},
		{"cataloged plugin event", "git.changed", "", "Git", "bolt", "the branch or worktree changes"},
		{"other plugin event", "processes.killed", "", "processes.killed", "bolt", "arrives"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Trigger{ID: "tr_1", Name: "x", Enabled: true, On: tt.on, Match: json.RawMessage(tt.match), Command: "notify.post", Args: json.RawMessage(`{}`),
				History: []Run{{At: at, Topic: tt.on, Status: automation.StatusOK, Chain: chain}}}
			view := mainView(tr, []*Trigger{tr}, map[string]string{"agt_1": "Coder"}, nil, automation.Open{}, automation.RunsView{})
			assert.Contains(t, texts(view), "what it waits for and what it does")
			cards, ok := find(view, "flow").Props["cards"].([]sdui.FlowCard)
			require.True(t, ok)
			assert.Equal(t, []string{tt.label, tt.icon, tt.out}, []string{cards[0].Label, cards[0].Icon, cards[0].Outs[0].Label})
		})
	}
}

func TestHelpPageListsEventsWithYourTriggers(t *testing.T) {
	turns := &Trigger{ID: "tr_1", Name: "Review", Enabled: true, On: "agents.turn,event.tidy.done", Command: automation.PromptCommand}
	graph := &Trigger{ID: "tr_2", Name: "Step", Enabled: true, On: "event.graph.gr_1.review", Command: automation.PromptCommand}
	view := mainView(nil, []*Trigger{turns, graph}, nil, nil, automation.Open{}, automation.RunsView{})
	require.NoError(t, isdui.Validate(view))
	assert.Equal(t, []string{automation.AskID}, childIDs(view.Children[0]))
	assert.Equal(t, "cmd+n", view.Children[0].Children[0].Props["shortcut"])

	rows, ok := find(view, "table").Props["rows"].([]sdui.TableRow)
	require.True(t, ok)
	yours := map[string]string{}
	for _, r := range rows {
		yours[r.ID] = r.Cells[3].Text
		assert.Equal(t, automation.ActAsk, r.Cells[4].Action)
	}
	assert.Equal(t, "1 trigger", yours["agents.turn"])
	assert.Equal(t, "1 trigger", yours["event.*"])
	assert.Equal(t, "1 trigger", yours["event.graph.*"])
	assert.Empty(t, yours["git.changed"])
	assert.Equal(t, "the branch or worktree changes", whenLine(&Trigger{On: "git.changed"}, nil))
}

func TestAskPromptNamesTheEvent(t *testing.T) {
	assert.Contains(t, askPrompt("event.*"), "fires when an agent or a cron job emits an event (on: event.<name>)")
	assert.Contains(t, askPrompt(""), "owner none")
	assert.NotContains(t, askPrompt(""), "fires when")
}
