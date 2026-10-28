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
)

func TestToolbar(t *testing.T) {
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 1, Comm: "sh"}},
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, Comm: "nvim", Args: []string{"nvim", strings.Repeat("é", 600)}}},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 2, Comm: "gopls"}, Orphan: true, Duplicate: true},
	}
	view, _ := mainView(viewModel{nodes: nodes, selected: 2, auto: true, sort: defaultSort, hist: newHistory()})
	toolbar := view.Children[0]
	assert.Equal(t, "Kill nvim (2) and its children?", toolbar.Children[0].Props["confirm"])
	assert.Equal(t, "3 processes · 1 orphans", toolbar.Children[4].Props["text"])

	rows, _ := processRows(viewModel{nodes: nodes, sort: defaultSort, hist: newHistory()})
	long := rows[1].Cells[len(columnIDs)-1].Text
	assert.Equal(t, cellRunes, utf8.RuneCountInString(long))
	assert.True(t, strings.HasSuffix(long, "é…"))
	assert.Equal(t, "gopls · orphan · dup", rows[2].Cells[len(columnIDs)-1].Text)

	idle, _ := mainView(viewModel{nodes: nodes, sort: defaultSort, hist: newHistory()})
	assert.Len(t, idle.Children[0].Children, 4)
	assert.Equal(t, "kill-orphans", idle.Children[0].Children[0].ID)

	empty, _ := mainView(viewModel{sort: defaultSort, scope: "All", hist: newHistory()})
	require.NoError(t, internalsdui.Validate(empty))
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
		nodes[i] = pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: i + 2, Comm: fmt.Sprint("p", i), CPU: float64(i % 100), RSSKiB: 1 << 20, StartTime: now}}
		for range recentLen {
			hist.push(processKey(nodes[i]), nodes[i].Process.CPU, 1024, now)
		}
	}
	view, _ := mainView(viewModel{nodes: nodes, selected: 2, sort: defaultSort, scope: "All", hist: hist, memKiB: 1 << 24, now: now})
	require.NoError(t, internalsdui.Validate(view))

	rows, _ := processRows(viewModel{nodes: nodes, sort: defaultSort, hist: hist, now: now})
	require.Len(t, rows, maxRows)
	more := rows[maxRows-1]
	assert.True(t, more.Muted)
	assert.Equal(t, "… 1001 more", more.Cells[len(more.Cells)-1].Text)
}
