package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	isdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/shortcuttest"
)

func TestSidebarDetailAndMenu(t *testing.T) {
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	daily := &Job{ID: "cj_a", Name: "a", Enabled: true, Schedule: Schedule{Cron: "0 9 * * *", Timezone: "UTC"}, NextRun: now.Add(2 * time.Hour)}
	paused := &Job{ID: "cj_b", Name: "b", Schedule: Schedule{Every: "1m"}, LastError: "boom", History: []Run{
		{At: now.Add(-time.Minute), Status: automation.StatusError},
		{At: now.Add(-2 * time.Minute), Status: automation.StatusOK},
	}}
	items := treeItems(t, sidebarView([]*Job{daily, paused}, "", now))
	require.Len(t, items, 3)
	assert.Equal(t, []any{automation.HelpID, true}, []any{items[0].ID, items[0].Muted})
	assert.Equal(t, "09:00", items[1].Detail)
	assert.Equal(t, "clock", items[1].Icon)
	assert.Equal(t, []string{"run", "pause", "edit", "duplicate", "delete"}, actions(items[1]))
	assert.Equal(t, "paused", items[2].Detail)
	assert.Equal(t, []string{"xmark.circle", "danger"}, []string{items[2].Icon, items[2].Tone})
	assert.Empty(t, items[2].Badge)
	assert.Equal(t, "resume", items[2].Menu[1].Action)
	assert.Equal(t, "Delete cron job “b”?", items[2].Menu[4].Confirm)
}

func TestMainHistoryNewestFirst(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	j := &Job{ID: "cj_a", Name: "a", Schedule: Schedule{Every: "1m", Timezone: "UTC"}, Owner: "agt_1", Command: agentNewCommand, Paused: []automation.Span{{From: at}}, History: []Run{
		{At: at.Add(time.Minute), Status: automation.StatusSkipped, Reason: automation.ReasonInFlight},
		{At: at, Status: automation.StatusOK, Result: `{"session":"agt_2"}`},
		{At: at.Add(2 * time.Minute), Status: automation.StatusError, Reason: "boom"},
	}}
	view := mainView(j, nil, map[string]string{"agt_1": "Orchestrator", "agt_2": "Child"}, nil, automation.Open{}, automation.RunsView{}, draft{}, at)
	require.NoError(t, isdui.Validate(view))
	assert.Contains(t, texts(view), "Orchestrator")
	assert.Equal(t, "header", view.Children[0].Type)
	assert.Equal(t, []string{"cj_a/resume", "cj_a/run", "cj_a/edit", "cj_a/duplicate", "cj_a/reveal", "cj_a/delete"}, childIDs(view.Children[0]))
	assert.Equal(t, sdui.VariantPrimary, view.Children[0].Children[0].Props["variant"])
	assert.Contains(t, texts(view), "none while paused")
	assert.Contains(t, texts(view), "paused by you, Oct 30")
	strip := nodesWithType(view, "runstrip")[0]
	assert.Equal(t, true, strip.Props["collapsed"])
	assert.Equal(t, []string{"table", "form"}, []string{strip.Children[0].Type, strip.Children[1].Type})
	rows := tableRows(t, view)
	require.Len(t, rows, 3)
	assert.Equal(t, []sdui.Cell{{Text: "Oct 30 09:02:00"}, {Text: "failed", Tone: "danger"}, {Text: "boom", Action: actShowLog, Label: "Show log"}}, rows[0].Cells)
	assert.Equal(t, "not run — the previous run was still running", rows[1].Cells[2].Text)
	assert.Equal(t, "started Child", rows[2].Cells[2].Text)
}

func TestMainSplitsMissedRunsFromThePolicy(t *testing.T) {
	now := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)
	j := &Job{ID: "cj_a", Name: "Nightly", Enabled: true, Missed: missedSkip, Command: "agents.new",
		Args:     json.RawMessage(`{"prompt":"Run govulncheck"}`),
		Schedule: Schedule{Cron: "0 3 * * *", Timezone: "UTC"}, NextRun: now.Add(13 * time.Hour),
		History: []Run{
			{At: now.Add(-48 * time.Hour), Status: automation.StatusOK},
			{At: now.Add(-11 * time.Hour), Status: automation.StatusSkipped, Reason: reasonMissed},
		}}

	view := mainView(j, nil, nil, nil, automation.Open{}, automation.RunsView{}, draft{}, now)
	require.NoError(t, isdui.Validate(view))
	assert.Equal(t, []sdui.HeaderBadge{{Text: "active"}, {Text: "missed 1 run", Tone: "warning"}}, view.Children[0].Props["badges"])
	got := texts(view)
	assert.Contains(t, got, "in 13 h")
	props := nodesWithType(view, "form")[0]
	policy := props.Children[2].Children[1]
	assert.Equal(t, []any{"select", missedSkip, actMissed}, []any{policy.Type, policy.Props["value"], policy.Props["action"]})
	assert.Contains(t, got, "the main checkout")
	assert.Contains(t, got, "nobody — failures go to the inbox")
	cards := nodesWithType(view, "card")
	banner := cards[len(cards)-1]
	assert.Equal(t, "warning", banner.Props["tone"])
	assert.Contains(t, texts(banner), "Missed Sat, Oct 31 · 03:00 — not run: Clubmate was closed or the Mac asleep")
	assert.Contains(t, childIDs(banner.Children[0]), "cj_a/runMissed")
}

func TestHelpPageCountsYourJobsPerSchedule(t *testing.T) {
	now := time.Now()
	jobs := []*Job{
		{ID: "cj_a", Schedule: Schedule{Cron: "0 9 * * *"}},
		{ID: "cj_b", Schedule: Schedule{At: now}},
		{ID: "cj_c", Schedule: Schedule{At: now.Add(time.Hour)}},
	}
	view := mainView(nil, jobs, nil, nil, automation.Open{}, automation.RunsView{}, draft{}, now)
	require.NoError(t, isdui.Validate(view))
	assert.Equal(t, []string{automation.AskID}, childIDs(view.Children[0]))
	for _, c := range nodesWithType(view, "flow")[0].Props["cards"].([]sdui.FlowCard) {
		assert.True(t, c.Ghost, c.ID)
	}
	table := nodesWithType(view, "table")[0]
	yours := map[string]string{}
	for _, r := range table.Props["rows"].([]sdui.TableRow) {
		yours[r.ID] = r.Cells[2].Text
		assert.Equal(t, automation.ActAsk, r.Cells[3].Action)
	}
	assert.Equal(t, map[string]string{"cron": "1 job", "every": "", "at": "2 jobs"}, yours)
}

func TestAskPromptNamesTheSchedule(t *testing.T) {
	assert.Contains(t, askPrompt("at"), "runs once, at a time or after a delay (at or in)")
	assert.Contains(t, askPrompt(""), "owner none")
	assert.NotContains(t, askPrompt(""), "that runs")
}

func TestDoesIsDrawnAndRunsSitOnAStrip(t *testing.T) {
	now := time.Date(2026, 11, 3, 16, 27, 0, 0, time.UTC)
	runs := []Run{
		{At: now.Add(-5 * time.Minute), Status: automation.StatusOK},
		{At: now.Add(-3 * time.Minute), Status: automation.StatusError, Reason: "403"},
		{At: now.Add(-time.Minute), Status: automation.StatusSkipped, Reason: reasonMissed},
	}
	poll := &Job{ID: "cj_a", Name: "Poll CI", Enabled: true, Command: "github.ci", Schedule: Schedule{Every: "2m", Timezone: "UTC"}, NextRun: now.Add(time.Minute), History: runs}
	titles := map[string]string{"github.ci": "Check CI status", "agt_1": "Backlog grooming"}

	view := mainView(poll, nil, titles, map[string]string{"agt_1": "idle"}, automation.Open{}, automation.RunsView{}, draft{}, now)
	require.NoError(t, isdui.Validate(view))
	flow := nodesWithType(view, "flow")[0]
	card := flow.Props["cards"].([]sdui.FlowCard)[0]
	assert.Equal(t, []string{"Check CI status", "plugin command"}, []string{card.Label, card.State})
	assert.Equal(t, []sdui.FlowOut{{ID: "ok", Label: "ok", Count: 1, State: sdui.EdgeTaken}, {ID: "failed", Label: "failed", Count: 1, State: sdui.EdgeLast}}, card.Outs)
	edges := flow.Props["edges"].([]sdui.FlowEdge)
	assert.Equal(t, []string{"when→action taken", "action.ok→runs taken", "action.failed→inbox last"}, edgeKeys(edges))
	strip := nodesWithType(view, "runstrip")[0]
	assert.Equal(t, []string{sdui.RunOK, sdui.RunFailed, sdui.RunMissed}, stripStatuses(strip))
	assert.Equal(t, []any{"Runs", 3}, []any{strip.Props["title"], strip.Props["count"]})
	assert.NotContains(t, strip.Props, "selected")
	assert.Equal(t, poll.NextRun.Format(time.RFC3339), strip.Props["next"])

	review := &Job{ID: "cj_b", Name: "Weekly review", Owner: "agt_1", Command: "agents.prompt", Args: json.RawMessage(`{"session":"agt_1","text":"groom"}`),
		Schedule: Schedule{Cron: "0 9 * * 1", Timezone: "UTC"}, History: runs[:1], Paused: []automation.Span{{From: now.Add(-2 * time.Minute)}}}
	view = mainView(review, nil, titles, map[string]string{"agt_1": "idle"}, automation.Open{}, automation.RunsView{Selected: automation.RunID(runs[0])}, draft{}, now)
	require.NoError(t, isdui.Validate(view))
	flow = nodesWithType(view, "flow")[0]
	edges = flow.Props["edges"].([]sdui.FlowEdge)
	assert.Equal(t, []string{"when→action dim", "action.ok→agt_1 dim", "action.failed→agt_1 never"}, edgeKeys(edges))
	assert.Equal(t, "paused", edges[1].Label)
	session := flow.Props["cards"].([]sdui.FlowCard)[1]
	assert.Equal(t, []string{"Backlog grooming", "idle · prompted Nov 3"}, []string{session.Label, session.State})
	strip = nodesWithType(view, "runstrip")[0]
	assert.Equal(t, automation.RunID(runs[0]), strip.Props["selected"])
	assert.NotContains(t, strip.Props, "next")
	assert.Equal(t, []sdui.PausedSpan{{From: now.Add(-2 * time.Minute)}}, strip.Props["paused"])
}

func nodesWithType(n sdui.Node, typ string) []sdui.Node {
	var out []sdui.Node
	if n.Type == typ {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, nodesWithType(c, typ)...)
	}
	return out
}

func edgeKeys(edges []sdui.FlowEdge) []string {
	keys := make([]string, 0, len(edges))
	for _, e := range edges {
		from := e.From
		if e.Out != "" {
			from += "." + e.Out
		}
		keys = append(keys, from+"→"+e.To+" "+e.State)
	}
	return keys
}

func stripStatuses(n sdui.Node) []string {
	var out []string
	for _, r := range n.Props["runs"].([]sdui.StripRun) {
		out = append(out, r.Status)
	}
	return out
}

func treeItems(t *testing.T, n sdui.Node) []sdui.TreeItem {
	t.Helper()
	require.Equal(t, "tree", n.Type)
	items, ok := n.Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	return items
}

func actions(item sdui.TreeItem) []string {
	out := make([]string, 0, len(item.Menu))
	for _, m := range item.Menu {
		out = append(out, m.Action)
	}
	return out
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

func tableRows(t *testing.T, n sdui.Node) []sdui.TableRow {
	t.Helper()
	if n.Type == "table" {
		rows, ok := n.Props["rows"].([]sdui.TableRow)
		require.True(t, ok)
		return rows
	}
	for _, c := range n.Children {
		if rows := tableRows(t, c); rows != nil {
			return rows
		}
	}
	return nil
}

func childIDs(n sdui.Node) []string {
	ids := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestWhenMissedPickerSetsThePolicy(t *testing.T) {
	now := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)
	j := &Job{ID: "cj_a", Name: "Nightly", Workspace: "ws", Enabled: true, Missed: missedOnce, Command: "notify.post", Schedule: Schedule{Cron: "0 3 * * *", Timezone: "UTC"}}
	a, _ := testApp(&now, nil, j)
	close(a.ready)

	require.NoError(t, a.setMissed(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws", Action: actMissed, Value: json.RawMessage(`"skip"`)}, j.ID))
	assert.Equal(t, missedSkip, a.jobs[j.ID].Missed)
	assert.Error(t, a.setMissed(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws", Action: actMissed, Value: json.RawMessage(`"later"`)}, j.ID))
}

func TestEditArgsKeepTheAgentArgsAndSendOnlyChanges(t *testing.T) {
	j := &Job{ID: "cj_a", Name: "Nightly", Command: agentNewCommand, Missed: missedOnce,
		Args:     json.RawMessage(`{"agent":"claude","prompt":"Audit","branch":"audit"}`),
		Schedule: Schedule{Cron: "0 3 * * *", Timezone: "UTC"}}
	require.NoError(t, isdui.Validate(editView(j, draft{})))
	values := editValues(j)
	values[fieldPrompt] = "Audit again"
	values[fieldBranch] = ""

	args, err := editArgs(j, values)
	require.NoError(t, err)
	assert.Equal(t, jobArgs{ID: "cj_a", Name: "Nightly", Missed: missedOnce, Args: `{"agent":"claude","prompt":"Audit again"}`}, args)
}

func TestViewShortcutsDeclared(t *testing.T) {
	j := &Job{ID: "cj_a", Name: "a", Enabled: true, Command: agentNewCommand, Schedule: Schedule{Every: "1m"}}
	shortcuttest.RequireDeclared(t, mainView(j, nil, nil, nil, automation.Open{}, automation.RunsView{}, draft{}, time.Now()), mainView(nil, nil, nil, nil, automation.Open{}, automation.RunsView{}, draft{}, time.Now()), editView(j, draft{}))
}
