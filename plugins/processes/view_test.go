package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func TestHeaderAndActionStrip(t *testing.T) {
	now := time.Date(2026, 10, 31, 13, 30, 0, 0, time.Local)
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 1, Comm: "sh"}},
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, Comm: "nvim", Args: []string{"/usr/local/bin/nvim", strings.Repeat("é", 600)}, StartTime: now.Add(-18 * time.Minute)}},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 2, Comm: "gopls"}, Orphan: true, Duplicate: true},
	}
	view, _ := mainView(viewModel{nodes: nodes, selected: 2, auto: true, sort: defaultSort, scope: "All", hist: newHistory(), now: now})
	require.NoError(t, internalsdui.Validate(view))
	header := view.Children[0]
	assert.Equal(t, "All processes", header.Props["title"])
	assert.Equal(t, "3 running · 1 orphans · auto-kill on", header.Props["meta"])
	assert.Nil(t, header.Children[0].Props["disabled"])

	strip := view.Children[len(view.Children)-1]
	require.Equal(t, "action-strip", strip.ID)
	summary := strip.Children[0].Children
	short := summary[0].Props["text"].(string)
	assert.Equal(t, cellRunes+3, utf8.RuneCountInString(short))
	assert.True(t, strings.HasPrefix(short, "2  nvim é"))
	assert.Equal(t, "child of 1 · started 13:12", summary[1].Props["text"])
	assert.Equal(t, "Kill nvim (2) and its children?", strip.Children[1].Props["confirm"])
	assert.Equal(t, killKey, strip.Children[1].Props["shortcut"])

	rows, _ := processRows(viewModel{nodes: nodes, sort: defaultSort, hist: newHistory()})
	long := rows[1].Cells[len(columnIDs)-1].Text
	assert.Equal(t, cellRunes, utf8.RuneCountInString(long))
	assert.Equal(t, "gopls · orphan · dup", rows[2].Cells[len(columnIDs)-1].Text)

	idle, _ := mainView(viewModel{nodes: nodes[:1], options: true, sort: defaultSort, scope: "All", hist: newHistory()})
	assert.Equal(t, true, idle.Children[0].Children[0].Props["disabled"])
	assert.Equal(t, "options-row", idle.Children[1].ID)
	assert.NotEqual(t, "action-strip", idle.Children[len(idle.Children)-1].ID)
}

func TestRowsGroupedByOwner(t *testing.T) {
	groups := []group{
		{ID: "ses_a", Title: "Terminal 1", Owner: "terminals"},
		{ID: "ses_b", Title: "Terminal 2", Owner: "terminals"},
		{ID: "root_c", Title: "Claude · clubmate", Owner: "agents"},
		{ID: "ses_idle", Title: "nvim", Owner: "editor-nvim"},
	}
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 10, Comm: "zsh"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 11, PPID: 10, Comm: "make"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 20, Comm: "zsh"}, SessionID: "ses_b"},
		{Process: pluginsdk.ProcessInfo{PID: 30, Comm: "claude"}, SessionID: "root_c"},
		{Process: pluginsdk.ProcessInfo{PID: 40, Comm: "stray"}, SessionID: "ses_gone"},
	}
	m := viewModel{nodes: nodes, groups: groups, sort: sdui.Sort{Column: colPID}, hist: newHistory()}
	rows, depth := processRows(m)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %d %s", r.ID, r.Depth, r.Cells[0].Text+r.Cells[len(columnIDs)-1].Text))
	}
	assert.Equal(t, []string{
		"g:terminals 0 Terminals",
		"10 1 10zsh · Terminal 1",
		"11 2 11make",
		"20 1 20zsh · Terminal 2",
		"g:root_c 0 Claude · clubmate",
		"30 1 30claude",
		"g:other 0 Other",
		"40 1 40stray",
	}, got)
	assert.Equal(t, 2, depth)
	assert.True(t, rows[0].Group)

	m.folded = map[string]bool{"terminals": true}
	rows, _ = processRows(m)
	assert.False(t, rows[0].Expanded)
	assert.Equal(t, "g:root_c", rows[1].ID)

	single, _ := processRows(viewModel{nodes: nodes[3:4], groups: groups[2:3], sort: defaultSort, hist: newHistory()})
	assert.Equal(t, []string{"30"}, []string{single[0].ID})
}

func TestProcessTableSortsSiblings(t *testing.T) {
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, CPU: 1}},
		{Process: pluginsdk.ProcessInfo{PID: 1, CPU: 0}},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 1, CPU: 5}},
		{Process: pluginsdk.ProcessInfo{PID: 4, PPID: 1, CPU: 3}},
	}
	rows, _ := processRows(viewModel{nodes: nodes, sort: defaultSort, hist: newHistory()})
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []string{"1", "3", "4", "2"}, ids)
}

func TestProcessTableCapped(t *testing.T) {
	now := time.Now()
	hist := newHistory()
	nodes := make([]pluginsdk.ProcessNode, 2000)
	for i := range nodes {
		nodes[i] = pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: i + 2, Comm: fmt.Sprint("p", i), CPU: float64(i % 100), RSSKiB: 1 << 20, StartTime: now}, SessionID: fmt.Sprint("ses_", i%2)}
		for range recentLen {
			hist.push(processKey(nodes[i]), nodes[i].Process.CPU, 1024, now)
		}
	}
	view, _ := mainView(viewModel{nodes: nodes, selected: 2, sort: defaultSort, scope: "All", hist: hist, memKiB: 1 << 24, now: now})
	require.NoError(t, internalsdui.Validate(view))

	groups := []group{{ID: "ses_0", Owner: "terminals"}, {ID: "ses_1", Owner: "agents"}}
	rows, _ := processRows(viewModel{nodes: nodes, groups: groups, sort: defaultSort, hist: hist, now: now})
	require.Len(t, rows, maxRows)
	more := rows[maxRows-1]
	assert.True(t, more.Muted)
	assert.Equal(t, "… 1002 more", more.Cells[len(more.Cells)-1].Text)
}
