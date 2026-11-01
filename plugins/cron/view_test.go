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
)

func TestSidebarDetailAndMenu(t *testing.T) {
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	daily := &Job{ID: "cj_a", Name: "a", Enabled: true, Schedule: Schedule{Cron: "0 9 * * *", Timezone: "UTC"}, NextRun: now.Add(2 * time.Hour)}
	paused := &Job{ID: "cj_b", Name: "b", Schedule: Schedule{Every: "1m"}, LastError: "boom", History: []Run{
		{At: now.Add(-time.Minute), Status: automation.StatusError},
		{At: now.Add(-2 * time.Minute), Status: automation.StatusOK},
	}}
	items := listItems(t, sidebarView([]*Job{daily, paused}, "", now))
	require.Len(t, items, 2)
	assert.Equal(t, "09:00", items[0].Detail)
	assert.Equal(t, "clock", items[0].Icon)
	assert.Equal(t, []string{"run", "pause", "duplicate", "delete"}, actions(items[0]))
	assert.Equal(t, "paused", items[1].Detail)
	assert.Equal(t, []string{"xmark.circle", "danger"}, []string{items[1].Icon, items[1].Tone})
	assert.Empty(t, items[1].Badge)
	assert.Equal(t, "resume", items[1].Menu[1].Action)
	assert.Equal(t, "Delete cron job “b”?", items[1].Menu[3].Confirm)
}

func TestMainHistoryNewestFirst(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	j := &Job{ID: "cj_a", Name: "a", Schedule: Schedule{Every: "1m"}, Owner: "agt_1", History: []Run{
		{At: at.Add(time.Minute), Status: automation.StatusSkipped, Reason: automation.ReasonInFlight},
		{At: at, Status: automation.StatusOK, Result: `{"session":"agt_2"}`},
		{At: at.Add(2 * time.Minute), Status: automation.StatusError, Reason: "boom"},
	}}
	view := mainView(j, map[string]string{"agt_1": "Orchestrator", "agt_2": "Child"}, automation.Open{}, at)
	require.NoError(t, isdui.Validate(view))
	assert.Contains(t, texts(view), "Orchestrator")
	assert.Equal(t, "header", view.Children[0].Type)
	assert.Equal(t, []string{"cj_a/run", "cj_a/resume", "cj_a/duplicate", "cj_a/reveal", "cj_a/delete"}, childIDs(view.Children[0]))
	rows := tableRows(t, view)
	require.Len(t, rows, 3)
	assert.Equal(t, "boom", rows[0].Cells[2].Text)
	assert.Equal(t, automation.ReasonInFlight, rows[1].Cells[2].Text)
	assert.Equal(t, `{"session":"Child"}`, rows[2].Cells[2].Text)
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

	view := mainView(j, nil, automation.Open{}, now)
	require.NoError(t, isdui.Validate(view))
	assert.Equal(t, []sdui.HeaderBadge{{Text: "active"}, {Text: "missed 1 run", Tone: "warning"}}, view.Children[0].Props["badges"])
	got := texts(view)
	assert.Contains(t, got, "in 13 h")
	missedRow := view.Children[1].Children[2]
	assert.Equal(t, "warning", missedRow.Children[1].Props["tone"])
	policy := view.Children[1].Children[3].Children[1]
	assert.Equal(t, []any{"select", missedSkip, actMissed}, []any{policy.Type, policy.Props["value"], policy.Props["action"]})
	assert.Contains(t, got, "the main checkout")
	assert.Contains(t, got, "nobody — results go to the inbox")
	assert.Contains(t, got, "Start an agent · Claude · new session")
	assert.Contains(t, childIDs(view.Children[1].Children[2]), "cj_a/runMissed")
}

func TestEmptyStates(t *testing.T) {
	assert.Equal(t, "empty", sidebarView(nil, "", time.Now()).Type)
	assert.Equal(t, "empty", mainView(nil, nil, automation.Open{}, time.Now()).Type)
}

func listItems(t *testing.T, n sdui.Node) []sdui.Item {
	t.Helper()
	require.Equal(t, "list", n.Type)
	items, ok := n.Props["items"].([]sdui.Item)
	require.True(t, ok)
	return items
}

func actions(item sdui.Item) []string {
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
