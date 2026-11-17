package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

func dockOf(t *testing.T, tree sdui.Node) []sdui.TreeDock {
	t.Helper()
	require.NoError(t, internalsdui.Validate(tree))
	dock, _ := nodesOf(tree, "tree")[0].Props["dock"].([]sdui.TreeDock)
	return dock
}

func findItem(items []sdui.TreeItem, id string) *sdui.TreeItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := findItem(items[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

func plusDock() sidebarPlus {
	return sidebarPlus{dock: []api.DockRow{{ID: dockService, Label: "Service", Detail: "0"}, {ID: dockGates, Label: "Land gates", Detail: "1"}, {ID: dockGraphs, Label: "Graphs", Detail: "1"}},
		gates: []sdui.TreeItem{{ID: dockGateHelp, Label: "How land gates work"}}, graphs: []sdui.TreeItem{{ID: dockGraphHelp, Label: "How graphs work"}, {ID: "graph:nightly", Label: "nightly", Open: "graph"}}}
}

func TestSidebarRunRowInWorktreeGroup(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review gate"}
	rr := &api.RunRow{ID: "gr_1", State: runRunning, Detail: "turn 2", Home: homeGroup}
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Run: run, Title: "Review gate · Coder", Status: statusIdle, CreatedAt: now, UpdatedAt: now}, RunRow: rr},
		{Rec: record{ID: "agt_r", Worktree: "/r/a", Run: run, Title: "Review gate · Reviewer", Status: statusIdle, CreatedAt: now.Add(time.Second), UpdatedAt: now}, RunRow: rr},
		{Rec: record{ID: "agt_x", Worktree: "/r/b", Title: "Solo", Status: statusDone, UpdatedAt: now}},
	}
	tree := sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plusDock(), nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	group := findItem(items, groupPrefix+"/r/a")
	require.NotNil(t, group)
	item := findItem(group.Children, runPrefix+"gr_1")
	require.NotNil(t, item)
	assert.Equal(t, []any{"run", "turn 2", []string{"agt_c", "agt_r"}}, []any{item.Open, item.Detail, ids(item.Children)})
}

func TestSidebarEmptiedRunInClosed(t *testing.T) {
	t.Parallel()
	now := time.Now()
	plus := plusDock()
	plus.runs = []api.RunRow{{ID: "gr_9", Title: "Nightly", State: runEmptied, Detail: "no sessions", Home: homeClosed, Sessions: []string{}}}
	rows := []sessionRow{{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}}}
	tree := sidebarView(rows, "", map[string]bool{closedKey: false}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plus, nil)
	require.NoError(t, internalsdui.Validate(tree))
	closed := findItem(nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem), groupPrefix+closedKey)
	require.NotNil(t, closed)
	item := findItem(closed.Children, runPrefix+"gr_9")
	require.NotNil(t, item)
	assert.Equal(t, []any{"Nightly", "no sessions", true, "run"}, []any{item.Label, item.Detail, item.Faint, item.Open})
}

func TestSidebarServiceInDock(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}},
		{Rec: record{ID: "agt_v", Worktree: "/r/a", Title: "Voice", Status: statusIdle, SystemPrompt: "x", UpdatedAt: now}},
	}
	plus := plusDock()
	plus.open = dockService
	tree := sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plus, nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Nil(t, findItem(items, groupPrefix+serviceKey))
	assert.Nil(t, findItem(items, "agt_v"))
	dock := dockOf(t, tree)
	assert.Equal(t, []string{dockService, dockGates, dockGraphs}, []string{dock[0].ID, dock[1].ID, dock[2].ID})
	assert.Equal(t, []string{"agt_v"}, ids(dock[0].Rows))
	assert.True(t, dock[0].Expanded)
	assert.Equal(t, 6, dock[1].Cap)
}

func TestDockOneOpenAtATime(t *testing.T) {
	t.Parallel()
	open := openDock("", dockGraphs, true)
	assert.Equal(t, dockGraphs, open)
	open = openDock(open, dockGates, true)
	assert.Equal(t, dockGates, open)
	assert.Empty(t, openDock(open, dockGates, false))
	assert.Equal(t, dockGates, openDock(open, dockGraphs, false))
}

func TestSearchUnfoldsDockMatch(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}}}
	search := sidebarSearch{query: "night", results: []searchResult{{Session: "agt_x", Title: "Solo", UpdatedAt: now}}}
	dock := dockOf(t, sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, search, 0, plusDock(), nil))
	i := slices.IndexFunc(dock, func(d sdui.TreeDock) bool { return d.ID == dockGraphs })
	assert.True(t, dock[i].Expanded)
	assert.Equal(t, []string{"graph:nightly"}, ids(dock[i].Rows))
}

func ids(items []sdui.TreeItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestGatePageRefusesAgents(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry(), uiState: newUIState()}
	_, err := a.pageEvent(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws", Slot: mainSlot, NodeID: "lg_1/" + actGatePause, Action: actGatePause, Agent: true}, gatePrefix+"lg_1")
	assert.ErrorIs(t, err, errUserAction)
}

func TestRemoveBySelectionAlwaysConfirms(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review gate"}
	ended := &api.RunRow{ID: "gr_1", State: runEnded, Detail: "approved", Home: homeGroup}
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/a", Run: run, Title: "Coder", Status: statusDone, CreatedAt: now, UpdatedAt: now}, RunRow: ended},
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Solo", Status: statusDone, UpdatedAt: now}},
	}
	plus := plusDock()
	plus.gates = append(plus.gates, sdui.TreeItem{ID: gatePrefix + "lg_1", Label: "shop", Menu: gateRemoveMenu("shop")})
	plus.open = dockGates
	tree := sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plus, nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	removal := func(item *sdui.TreeItem) sdui.MenuEntry {
		require.NotNil(t, item)
		i := slices.IndexFunc(item.Menu, func(e sdui.MenuEntry) bool { return e.Key == removeKey })
		require.GreaterOrEqual(t, i, 0, item.ID)
		return item.Menu[i]
	}
	for _, entry := range []sdui.MenuEntry{removal(findItem(items, "agt_x")), removal(findItem(items, runPrefix+"gr_1")), removal(findItem(dockOf(t, tree)[1].Rows, gatePrefix+"lg_1"))} {
		assert.NotEmpty(t, entry.Confirm, entry.Action)
	}
}

func TestRunInTwoScopesKeepsIDsUnique(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := &api.Run{ID: "gr_1", Title: "Review gate"}
	rr := &api.RunRow{ID: "gr_1", State: runRunning, Detail: "turn 2", Home: homeGroup}
	rows := []sessionRow{
		{Rec: record{ID: "agt_c", Worktree: "/r/wt", Run: run, Title: "Coder", Status: statusIdle, CreatedAt: now, UpdatedAt: now}, RunRow: rr},
		{Rec: record{ID: "agt_r", Worktree: "/r/a", Run: run, Title: "Reviewer", Status: statusIdle, CreatedAt: now, UpdatedAt: now}, RunRow: rr},
		{Rec: record{ID: "agt_t", Worktree: "/r/a", Run: run, Title: "Tester", Status: statusClosed, CreatedAt: now, UpdatedAt: now}, RunRow: rr},
	}
	tree := sidebarView(rows, "run:gr_1", map[string]bool{closedKey: false}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plusDock(), nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.NotNil(t, findItem(items, "run:gr_1"))
	assert.Equal(t, []string{"agt_c", "agt_r", "agt_t"}, shownSessions(items))
	assert.Equal(t, "gr_1", runOf("run:gr_1@2"))
	assert.Equal(t, foldKey("run:gr_1"), foldKey("run:gr_1@2"))
}

func TestEmptiedRunShowsWithoutSessions(t *testing.T) {
	t.Parallel()
	plus := plusDock()
	plus.runs = []api.RunRow{{ID: "gr_9", Title: "Nightly", State: runEmptied, Detail: "no sessions", Home: homeClosed, Sessions: []string{}}}
	tree := sidebarView(nil, "", map[string]bool{closedKey: false}, time.Now(), "/r/a", nil, false, filterAll, sidebarSearch{}, 0, plus, nil)
	require.NoError(t, internalsdui.Validate(tree))
	closed := findItem(nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem), groupPrefix+closedKey)
	require.NotNil(t, closed)
	assert.NotNil(t, findItem(closed.Children, runPrefix+"gr_9"))
}

func TestSearchListsAServiceSessionOnce(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rows := []sessionRow{{Rec: record{ID: "agt_v", Worktree: "/r/a", Title: "Voice", Status: statusIdle, SystemPrompt: "x", UpdatedAt: now}}}
	search := sidebarSearch{query: "voice", results: []searchResult{{Session: "agt_v", Title: "Voice", UpdatedAt: now}}}
	raw, err := json.Marshal(sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, search, 0, plusDock(), nil))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(raw), `"id":"agt_v"`))
}

func TestOpenTabKeepsMainEventsOffAPage(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry(), uiState: newUIState()}
	p := a.workspaceState("ws")
	p.selected = gatePrefix + "lg_1"
	_, ok := a.shownPage("ws")
	assert.True(t, ok)
	p.filter = filterOpen
	_, ok = a.shownPage("ws")
	assert.False(t, ok)
}
