package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func TestAsideNotRestricted(t *testing.T) {
	assert.False(t, record{Aside: &asideInfo{}, DisallowedTools: asideDenied}.restricted())
}

func TestAsideState(t *testing.T) {
	cases := []struct {
		name string
		rec  record
		want string
	}{
		{"running", record{Status: statusRunning, Aside: &asideInfo{}}, asideThinking},
		{"waiting on its own question", record{Status: statusWaiting, Asks: true, Aside: &asideInfo{}}, asideAsks},
		{"idle after a send", record{Status: statusIdle, Aside: &asideInfo{Sent: true}}, asideSent},
		{"idle", record{Status: statusDone, Aside: &asideInfo{}}, asideOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, asideState(tc.rec))
		})
	}
}

func TestAsideSeedText(t *testing.T) {
	cases := []struct {
		name   string
		anchor asideAnchor
		want   string
	}{
		{"option", asideAnchor{Option: "Postgres", Question: "Which database?"}, `discuss option "Postgres" of the question "Which database?". Don't`},
		{"passage", asideAnchor{Message: "m1", Quote: "gives way to the title"}, `discuss this passage: "gives way to the title". Don't`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, asideSeed(tc.anchor), tc.want)
		})
	}
}

func asideRows(n int) []sessionRow {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	parent := record{ID: "p", Worktree: "/w", Title: "Parent", Status: statusDone, CreatedAt: at}
	rows := []sessionRow{{Rec: parent}, {Rec: record{ID: "c", Worktree: "/w", Title: "Child", Status: statusDone, ParentSession: "p", CreatedAt: at.Add(time.Minute)}}}
	for i := range n {
		an := asideAnchor{Option: "Option " + strconv.Itoa(i)}
		rows = append(rows, sessionRow{Rec: record{ID: "a" + strconv.Itoa(i), Worktree: "/w", Title: asideLabel(an), Status: statusIdle, ParentSession: "p", Aside: &asideInfo{Anchor: an}, CreatedAt: at.Add(time.Duration(2+i) * time.Minute)}})
	}
	return rows
}

func parentItem(t *testing.T, rows []sessionRow) sdui.TreeItem {
	t.Helper()
	view := sidebarView(rows, "", map[string]bool{expandPrefix + "p": false}, time.Now(), "/w", false, "", sidebarSearch{}, 0, sidebarPlus{})
	tree := view.Children[len(view.Children)-1]
	items, ok := tree.Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	i := slices.IndexFunc(items, func(it sdui.TreeItem) bool { return it.ID == "p" })
	require.GreaterOrEqual(t, i, 0)
	return items[i]
}

func TestAsideRowsOrder(t *testing.T) {
	item := parentItem(t, asideRows(2))
	ids := make([]string, 0, len(item.Children))
	for _, c := range item.Children {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"c", "a1", "a0"}, ids)
	assert.Equal(t, "Aside · Option 1", item.Children[1].Label)
	assert.Equal(t, "open", item.Children[1].Detail)
	assert.Equal(t, asideIcon, item.Children[1].Icon)
}

func TestAsidesFold(t *testing.T) {
	item := parentItem(t, asideRows(3))
	require.Len(t, item.Children, 2)
	assert.Equal(t, "Asides · 3", item.Children[1].Label)
}

func TestAsideAsksListsItsParent(t *testing.T) {
	rows := asideRows(1)
	rows[2].Rec.Status = statusWaiting
	view := sidebarView(rows, "", map[string]bool{}, time.Now(), "/w", false, filterNeeds, sidebarSearch{}, 0, sidebarPlus{})
	tree := view.Children[len(view.Children)-1]
	items, ok := tree.Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	require.Len(t, items, 1)
	assert.Equal(t, "p", items[0].ID)

	infos := []sessionInfo{{record: rows[0].Rec}, {record: rows[2].Rec, NeedsYou: true}}
	setGroups(infos)
	assert.Equal(t, groupNeedsYou, infos[0].Group)
	infos = []sessionInfo{{record: rows[0].Rec}, {record: record{ID: "a", ParentSession: "p", Status: statusRunning, Aside: &asideInfo{}}}}
	setGroups(infos)
	assert.Equal(t, groupDone, infos[0].Group)
}

func TestAsideLeavesTaskSummaryAndArchivable(t *testing.T) {
	a := &app{registry: newRegistry(), uiState: newUIState(), reviews: newReviewBoard(), watchdog: newWatchdog()}
	dir := t.TempDir()
	add := func(rec record) {
		a.sessions[rec.ID] = newSession(rec, newEventLog(filepath.Join(dir, rec.ID+".jsonl")), true, func(string, bool) {})
	}
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	add(record{ID: "boss", Workspace: "w", Status: statusDone, CreatedAt: at, Task: &taskRecord{Slug: "pause", Repos: []taskRepo{{Path: "/ads", Name: "ads", Session: "kid", State: landMerged}}}})
	add(record{ID: "kid", Workspace: "w", Status: statusDone, ParentSession: "boss", RepoPath: "/ads", CreatedAt: at.Add(time.Minute)})
	boss := func() sessionInfo {
		infos := a.infos("w")
		return infos[slices.IndexFunc(infos, func(i sessionInfo) bool { return i.ID == "boss" })]
	}
	before := boss()
	require.True(t, before.Archivable)

	add(record{ID: "aside", Workspace: "w", Status: statusRunning, ParentSession: "boss", Aside: &asideInfo{}, CreatedAt: at.Add(2 * time.Minute)})
	after := boss()
	assert.True(t, after.Archivable)
	assert.Equal(t, before.TaskSummary, after.TaskSummary)
	assert.Equal(t, before.TaskRows, after.TaskRows)
}

func treeItems(t *testing.T, rows []sessionRow, collapsed map[string]bool, filter string) []sdui.TreeItem {
	t.Helper()
	view := sidebarView(rows, "", collapsed, time.Now(), "/w", false, filter, sidebarSearch{}, 0, sidebarPlus{})
	items, ok := view.Children[len(view.Children)-1].Props["items"].([]sdui.TreeItem)
	require.True(t, ok)
	return items
}

func TestAsidesHangUnderATaskParent(t *testing.T) {
	rows := asideRows(1)
	rows[0].Rec.Task = &taskRecord{Slug: "pause", Repos: []taskRepo{{Path: "/ads", Name: "ads", Session: "c", State: landWaits}}}
	items := treeItems(t, rows, map[string]bool{expandPrefix + "p": false}, "")
	i := slices.IndexFunc(items, func(it sdui.TreeItem) bool { return it.ID == "p" })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, slices.ContainsFunc(items[i].Children, func(it sdui.TreeItem) bool { return it.ID == "a0" }))
}

func TestNestedAsideAskingListsTheRoot(t *testing.T) {
	rows := asideRows(1)
	rows[2].Rec.ParentSession, rows[2].Rec.Status = "c", statusWaiting
	items := treeItems(t, rows, map[string]bool{}, filterNeeds)
	assert.True(t, slices.ContainsFunc(items, func(it sdui.TreeItem) bool { return it.ID == "p" }))
}

func TestDeleteConfirmCountsNoAsides(t *testing.T) {
	item := parentItem(t, asideRows(2))
	i := slices.IndexFunc(item.Menu, func(m sdui.MenuEntry) bool { return m.Action == actDelete })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, fmt.Sprintf(deleteTree, 1), item.Menu[i].Confirm)
}
