package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	items := listItems(t, sidebarView([]*Job{daily, paused}, nil, "", now))
	require.Len(t, items, 2)
	assert.Equal(t, "every day 09:00 · next 09:00", items[0].Detail)
	assert.Equal(t, "clock", items[0].Icon)
	assert.Empty(t, items[0].Badge)
	assert.Equal(t, []string{"pause", "run", "delete"}, actions(items[0]))
	assert.Equal(t, "paused · boom", items[1].Detail)
	assert.Equal(t, "pause.circle", items[1].Icon)
	assert.Equal(t, "failed", items[1].Badge)
	assert.Equal(t, []string{"resume", "run", "delete"}, actions(items[1]))
	assert.Equal(t, "Delete cron job “b”?", items[1].Menu[2].Confirm)
}

func TestMainHistoryNewestFirst(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	j := &Job{ID: "cj_a", Name: "a", Schedule: Schedule{Every: "1m"}, Owner: "agt_1", History: []Run{
		{At: at.Add(time.Minute), Status: automation.StatusSkipped, Reason: automation.ReasonInFlight},
		{At: at, Status: automation.StatusOK, Result: `{"session":"agt_2"}`},
		{At: at.Add(2 * time.Minute), Status: automation.StatusError, Reason: "boom"},
	}}
	view := mainView(j, map[string]string{"agt_1": "Orchestrator"})
	assert.Contains(t, texts(view), "Owner: Orchestrator")
	rows := tableRows(t, view)
	require.Len(t, rows, 3)
	assert.Equal(t, "boom", rows[0].Cells[2].Text)
	assert.Equal(t, automation.ReasonInFlight, rows[1].Cells[2].Text)
	assert.Equal(t, `{"session":"agt_2"}`, rows[2].Cells[2].Text)
}

func TestEmptyStates(t *testing.T) {
	assert.Equal(t, "empty", sidebarView(nil, nil, "", time.Now()).Type)
	assert.Equal(t, "empty", mainView(nil, nil).Type)
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
