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
	"clubmate/plugins/internal/shortcuttest"
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
	assert.Equal(t, true, header.Children[0].Props["live"])
	assert.Nil(t, header.Children[1].Props["disabled"])

	strip := view.Children[len(view.Children)-1]
	require.Equal(t, "action-strip", strip.ID)
	summary := strip.Children[0].Children
	short := summary[0].Props["text"].(string)
	assert.Equal(t, cellRunes+3, utf8.RuneCountInString(short))
	assert.True(t, strings.HasPrefix(short, "2  nvim é"))
	assert.Equal(t, "child of 1 · started 13:12", summary[1].Props["text"])
	assert.Equal(t, "/usr/local/bin/nvim '"+strings.Repeat("é", 600)+"'", strip.Children[1].Props["copy"])
	assert.Nil(t, strip.Children[2].Props["disabled"])
	assert.Equal(t, "Kill nvim (2) and its children?", strip.Children[3].Props["confirm"])
	assert.Equal(t, killKey, strip.Children[3].Props["shortcut"])
	shortcuttest.RequireDeclared(t, view)

	rows, _ := processRows(viewModel{nodes: nodes, sort: defaultSort, hist: newHistory()})
	long := rows[1].Cells[len(columnIDs)-1].Text
	assert.Equal(t, cellRunes, utf8.RuneCountInString(long))
	assert.Equal(t, "gopls · orphan · dup", rows[2].Cells[len(columnIDs)-1].Text)

	idle, _ := mainView(viewModel{nodes: nodes[:1], options: true, sort: defaultSort, scope: "All", hist: newHistory()})
	assert.Equal(t, true, idle.Children[0].Children[1].Props["disabled"])
	assert.Equal(t, "Hide options", idle.Children[0].Children[2].Props["label"])
	assert.Equal(t, true, idle.Children[0].Children[2].Props["iconOnly"])
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
		{Process: pluginsdk.ProcessInfo{PID: 10, Comm: "zsh", CPU: 1.5, RSSKiB: 1024}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 11, PPID: 10, Comm: "make", CPU: 40, RSSKiB: 2048}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 20, Comm: "zsh", CPU: 0.25, RSSKiB: 1024}, SessionID: "ses_b"},
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
	assert.Equal(t, "Terminals · 41.8% · 4 MB", rows[0].Cells[0].Text)
	assert.Equal(t, "g:root_c", rows[1].ID)

	single, _ := processRows(viewModel{nodes: nodes[3:4], groups: groups[2:3], sort: defaultSort, hist: newHistory()})
	assert.Equal(t, []string{"30"}, []string{single[0].ID})
}

func TestCoreGroupFirstAndReadOnly(t *testing.T) {
	groups := []group{{ID: "ses_a", Title: "Terminal 1", Owner: "terminals"}, coreGroup("w")}
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 10, Comm: "zsh"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 316, Comm: "clubmate-core"}, SessionID: coreID},
		{Process: pluginsdk.ProcessInfo{PID: 324, PPID: 316, Comm: "cm-plugin-agents"}, SessionID: coreID},
	}
	view, _ := mainView(viewModel{nodes: nodes, groups: groups, selected: 324, sort: defaultSort, scope: "All", hist: newHistory()})
	require.NoError(t, internalsdui.Validate(view))
	rows := view.Children[2].Children[0].Props["rows"].([]sdui.TableRow)
	assert.Equal(t, "g:core", rows[0].ID)
	assert.False(t, rows[1].Expanded)
	assert.Len(t, rows, 4)
	strip := view.Children[len(view.Children)-1]
	assert.Equal(t, true, strip.Children[3].Props["disabled"])
	assert.Equal(t, true, strip.Children[2].Props["disabled"])
}

func TestFilterKeepsAncestorsAndMatchesGroupTitles(t *testing.T) {
	groups := []group{{ID: "ses_a", Title: "Terminal 1", Owner: "terminals"}, {ID: "root_c", Title: "Claude · clubmate", Owner: "agents"}}
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 10, Comm: "zsh"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 11, PPID: 10, Comm: "make"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 12, PPID: 11, Comm: "go", Args: []string{"go", "test"}}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 13, PPID: 10, Comm: "vim"}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 30, Comm: "node"}, SessionID: "root_c"},
	}
	pids := func(query string) []int {
		var out []int
		for _, n := range filterNodes(nodes, groups, query) {
			out = append(out, n.Process.PID)
		}
		return out
	}
	assert.Equal(t, []int{10, 11, 12}, pids(" GO TEST "))
	assert.Equal(t, []int{30}, pids("claude"))

	view, _ := mainView(viewModel{nodes: nodes, groups: groups, filter: "nothing", selected: 30, sort: defaultSort, scope: "All", hist: newHistory()})
	assert.Equal(t, "no-match", view.Children[2].ID)
	assert.NotEqual(t, "action-strip", view.Children[len(view.Children)-1].ID)
}

func TestCoreMembersSkipSessionHostsAndTrackedTrees(t *testing.T) {
	procs := []pluginsdk.ProcessInfo{
		{PID: 1, Comm: "launchd"},
		{PID: 316, PPID: 1, Comm: "clubmate-core"},
		{PID: 324, PPID: 316, Comm: "cm-plugin-agents"},
		{PID: 400, PPID: 324, Comm: "claude"},
		{PID: 401, PPID: 400, Comm: "node"},
		{PID: 500, PPID: 316, Comm: sessionHostComm},
		{PID: 501, PPID: 500, Comm: "zsh"},
		{PID: 600, PPID: 1, Comm: "Finder"},
	}
	var got []int
	for _, p := range coreMembers(procs, 316, []pluginsdk.ProcessNode{{Process: pluginsdk.ProcessInfo{PID: 400}}}) {
		got = append(got, p.PID)
	}
	assert.Equal(t, []int{316, 324}, got)
}

func TestFoldedRowShowsSubtreeTotals(t *testing.T) {
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 1, CPU: 1, RSSKiB: 50 * kibPerMiB}, SessionID: coreID},
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, CPU: 0, RSSKiB: 5 * kibPerMiB}, SessionID: coreID},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 2, CPU: 30, RSSKiB: 350 * kibPerMiB}, SessionID: coreID},
		{Process: pluginsdk.ProcessInfo{PID: 4, PPID: 1, CPU: 2, RSSKiB: 200 * kibPerMiB}, SessionID: coreID},
	}
	m := viewModel{nodes: nodes, sort: defaultSort, hist: newHistory(), collapsed: map[int]bool{2: true}}
	cells := func(rows []sdui.TableRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.ID+" "+r.Cells[1].Text+" "+r.Cells[2].Text)
		}
		return out
	}
	folded, _ := processRows(m)
	assert.Equal(t, []string{"1 33.0 605 MB"}, cells(folded))
	m.coreOpen = true
	open, _ := processRows(m)
	assert.Equal(t, []string{"1 1.0 50 MB", "2 30.0 355 MB", "4 2.0 200 MB"}, cells(open))
}

func TestFoldedRowHistoryIsSubtreeHistory(t *testing.T) {
	parent := pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: 1, CPU: 10}, SessionID: "s"}
	child := pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, CPU: 30}, SessionID: "s"}
	sibling := pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{PID: 3, CPU: 20}, SessionID: "s"}
	hist := newHistory()
	now := time.Now()
	for i, nodes := range [][]pluginsdk.ProcessNode{{parent, sibling}, {parent, sibling}, {parent, child, sibling}, {parent, child, sibling}} {
		hist.record(nil, nodes, nil, nil, now.Add(time.Duration(i)*time.Second))
	}
	m := viewModel{nodes: []pluginsdk.ProcessNode{parent, child, sibling}, sort: sdui.Sort{Column: colHistory, Desc: true}, hist: hist}
	history := func(rows []sdui.TableRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprint(r.ID, r.Cells[4].Spark.Values))
		}
		return out
	}

	m.collapsed = map[int]bool{1: true}
	folded, _ := processRows(m)
	assert.Equal(t, []string{"1[10 10 40 40]", "3[20 20 20 20]"}, history(folded))

	m.collapsed = nil
	open, _ := processRows(m)
	assert.Equal(t, []string{"3[20 20 20 20]", "1[10 10 10 10]", "2[30 30]"}, history(open))
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

func TestSidebarShowsOwnerIconAndCPU(t *testing.T) {
	groups := []group{{ID: "ses_a", Title: "Terminal 1", Owner: "terminals"}, {ID: "reg", Title: "Hook", Owner: "hooks"}, coreGroup("w")}
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 10, CPU: 1.25}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 11, CPU: 2}, SessionID: "ses_a"},
		{Process: pluginsdk.ProcessInfo{PID: 316, CPU: 0.4}, SessionID: coreID},
	}
	items := sidebarView(groups, sessionCPU(groups, nodes), len(nodes), "").Props["items"].([]sdui.Item)
	var got []string
	for _, it := range items {
		got = append(got, fmt.Sprintf("%s|%s|%s", it.Icon, it.Label, it.Detail))
	}
	assert.Equal(t, []string{"|All|3", "terminal|Terminal 1|3.2%", "gearshape|Hook|0.0%", "cpu|Clubmate core|0.4%"}, got)
}

func TestCommandShortensPathsKeepingExecutableAndArgs(t *testing.T) {
	n := pluginsdk.ProcessNode{Process: pluginsdk.ProcessInfo{Args: []string{
		"/Users/cali/develop/clubmate/build/bin/nvim", "--embed", "--listen=/Users/cali/Library/Caches/clubmate/editor-nvim/prj_1a3983d56.sock", "/tmp/x",
	}}}
	assert.Equal(t, "/Users/…/bin/nvim --embed --listen=/Users/…/editor-nvim/prj_1a3983d56.sock /tmp/x", displayCommand(n, cellRunes))
}
