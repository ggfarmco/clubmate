package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
)

func numbersOf(items []sdui.TreeItem) map[string]int {
	out := map[string]int{}
	var walk func([]sdui.TreeItem)
	walk = func(items []sdui.TreeItem) {
		for _, item := range items {
			if item.Number > 0 {
				out[item.ID] = item.Number
			}
			walk(item.Children)
		}
	}
	walk(items)
	return out
}

func TestNumbersGoToShownTopLevelRowsOnly(t *testing.T) {
	t.Parallel()
	rows := asideRows(1)
	at := rows[len(rows)-1].Rec.CreatedAt
	run := &api.Run{ID: "gr_1", Title: "Pair"}
	for i, rec := range []record{
		{ID: "s1", Worktree: "/w", Status: statusDone, Run: run},
		{ID: "s2", Worktree: "/w", Status: statusDone, Run: run},
		{ID: "q", Worktree: "/r/b", Status: statusDone},
		{ID: "r", Worktree: "/w", Status: statusRunning},
	} {
		rec.CreatedAt = at.Add(time.Duration(i+1) * time.Minute)
		rows = append(rows, sessionRow{Rec: rec})
	}
	want := map[string]int{"p": 1, runPrefix + "gr_1": 2, "r": 3}

	assert.Equal(t, want, numbersOf(treeItems(t, rows, map[string]bool{"/r/b": true}, "")))
	assert.Equal(t, want, numbersOf(treeItems(t, rows, map[string]bool{"/r/b": true, expandPrefix + "p": false}, "")))
	assert.Equal(t, map[string]int{"r": 1}, numbersOf(treeItems(t, rows, map[string]bool{}, filterRunning)))
}

func TestNeedsGoOldestFirstAndWrap(t *testing.T) {
	t.Parallel()
	since := func(m int) *time.Time {
		at := time.Date(2026, 11, 25, 12, m, 0, 0, time.UTC)
		return &at
	}
	infos := []sessionInfo{
		{record: record{ID: "agt_new"}, NeedsYou: true, NeedsYouSince: since(3)},
		{record: record{ID: "agt_calm"}},
		{record: record{ID: "agt_late"}, NeedsYou: true},
		{record: record{ID: "agt_hidden"}, NeedsYou: true, NeedsYouSince: since(0), Hidden: true},
		{record: record{ID: "gr_1"}, Kind: kindRun, NeedsYou: true, NeedsYouSince: since(2)},
		{record: record{ID: "agt_old"}, NeedsYou: true, NeedsYouSince: since(1)},
	}
	ids := needsIDs(infos)
	setNeedsRanks(infos)

	assert.Equal(t, []string{"agt_old", runPrefix + "gr_1", "agt_new", "agt_late"}, ids)
	assert.Equal(t, []int{3, 0, 4, 0, 2, 1}, []int{infos[0].NeedsRank, infos[1].NeedsRank, infos[2].NeedsRank, infos[3].NeedsRank, infos[4].NeedsRank, infos[5].NeedsRank})
	assert.Equal(t, "agt_old", stepNeeds(ids, "agt_calm", 1))
	assert.Equal(t, "agt_late", stepNeeds(ids, "agt_calm", -1))
	assert.Equal(t, "agt_old", stepNeeds(ids, "agt_late", 1))
	assert.Empty(t, stepNeeds(nil, "agt_calm", 1))
}

func TestARunInTwoWorktreesGetsOneNumber(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	run := &api.Run{ID: "gr_1", Title: "Pair"}
	rows := []sessionRow{
		{Rec: record{ID: "s1", Worktree: "/w", Status: statusDone, Run: run, CreatedAt: at}},
		{Rec: record{ID: "s2", Worktree: "/r/b", Status: statusDone, Run: run, CreatedAt: at.Add(time.Minute)}},
		{Rec: record{ID: "q", Worktree: "/r/b", Status: statusDone, CreatedAt: at.Add(2 * time.Minute)}},
	}

	assert.Equal(t, map[string]int{runPrefix + "gr_1": 1, "q": 2}, numbersOf(treeItems(t, rows, map[string]bool{}, "")))
}

func TestRunRowsAreNumberedOnlyAtTheTop(t *testing.T) {
	t.Parallel()
	infos := map[string]sessionInfo{
		"lander": {record: record{ID: "lander"}},
		"s1":     {record: record{ID: "s1", Status: statusRunning}},
		"r1":     {record: record{ID: "r1", Status: statusRunning}, NestUnder: "lander"},
	}
	top := runRow(graphs.RunSnapshot{ID: "gr_1", Status: graphRunning, Nodes: []graphs.SnapshotNode{{Session: "s1"}}}, infos)
	under := runRow(graphs.RunSnapshot{ID: "gr_2", Status: graphRunning, Nodes: []graphs.SnapshotNode{{Session: "r1"}}}, infos)

	assert.True(t, top.Numbered)
	assert.False(t, top.NeedsYou)
	assert.False(t, under.Numbered)
}
