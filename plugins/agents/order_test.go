package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/sdui"
)

const orderHome = "/r/m"

func orderRecs(t0 time.Time) []record {
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	return []record{
		{ID: "agt_b1", Worktree: "/r/b", Status: statusDone, CreatedAt: at(1), UpdatedAt: at(1)},
		{ID: "agt_a1", Worktree: "/r/a", Status: statusDone, CreatedAt: at(2), UpdatedAt: at(2)},
		{ID: "agt_m1", Worktree: orderHome, Status: statusDone, CreatedAt: at(3), UpdatedAt: at(3)},
		{ID: "agt_b2", Worktree: "/r/b", Status: statusDone, CreatedAt: at(4), UpdatedAt: at(4)},
		{ID: "agt_c1", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_b2"}, Status: statusDone, CreatedAt: at(5), UpdatedAt: at(5)},
		{ID: "agt_c2", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_b2"}, Status: statusRunning, CreatedAt: at(6), UpdatedAt: at(6)},
	}
}

func sidebarOrder(recs []record) []string {
	recs = slices.Clone(recs)
	slices.SortFunc(recs, startedFirst)
	rows := make([]sessionRow, 0, len(recs))
	for _, rec := range recs {
		rows = append(rows, sessionRow{Rec: rec})
	}
	tree := sidebarView(rows, "", map[string]bool{expandPrefix + "agt_b2": false, closedKey: false}, time.Now(), orderHome, groupKeys(recs, orderHome), false, "", sidebarSearch{}, 0, sidebarPlus{}, nil)
	var walk func(items []sdui.TreeItem) []string
	walk = func(items []sdui.TreeItem) []string {
		var out []string
		for _, item := range items {
			out = append(out, item.ID)
			out = append(out, walk(item.Children)...)
		}
		return out
	}
	return walk(nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem))
}

func TestSidebarOrderIgnoresActivityAndState(t *testing.T) {
	t.Parallel()
	recs := orderRecs(time.Now())
	before := sidebarOrder(recs)
	assert.Equal(t, []string{"wt:" + orderHome, "agt_m1", "wt:/r/b", "agt_b1", "agt_b2", "agt_c1", "agt_c2", "wt:/r/a", "agt_a1"}, before)

	recs[0].UpdatedAt = recs[0].UpdatedAt.Add(time.Hour)
	recs[1].Status = statusRunning
	recs[3].Status = statusRunning
	assert.Equal(t, before, sidebarOrder(recs))
}

func TestSidebarNewSessionAppendsToItsGroup(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	recs := orderRecs(t0)
	recs[0].Status = statusClosed
	recs = append(recs,
		record{ID: "agt_a0", Worktree: "/r/a", Status: statusRunning, CreatedAt: t0.Add(7 * time.Second)},
		record{ID: "agt_c3", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_b2"}, Status: statusRunning, CreatedAt: t0.Add(8 * time.Second)},
	)
	assert.Equal(t, []string{"wt:" + orderHome, "agt_m1", "wt:/r/b", "agt_b2", "agt_c1", "agt_c2", "agt_c3", "wt:/r/a", "agt_a1", "agt_a0", "wt:" + closedKey, "agt_b1"}, sidebarOrder(recs))
}

func TestListKeysStayPutAndSortLikeStart(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	infosOf := func(recs []record) []sessionInfo {
		slices.SortFunc(recs, startedFirst)
		out := make([]sessionInfo, 0, len(recs))
		for _, rec := range recs {
			out = append(out, sessionInfo{record: rec})
		}
		setOrder(out, orderHome)
		return out
	}
	keysOf := func(infos []sessionInfo) map[string][2]string {
		out := map[string][2]string{}
		for _, info := range infos {
			out[info.ID] = [2]string{info.ListKey, info.ListGroupKey}
		}
		return out
	}
	recs := []record{
		{ID: "agt_2", Worktree: "/r/b", CreatedAt: t0},
		{ID: "agt_1", Worktree: "/r/b", CreatedAt: t0},
		{ID: "agt_3", Worktree: "/r/a", CreatedAt: t0.Add(500 * time.Millisecond)},
		{ID: "agt_4", Worktree: orderHome, CreatedAt: t0.Add(time.Second)},
	}
	before := infosOf(slices.Clone(recs))
	byKey := slices.Clone(before)
	slices.SortFunc(byKey, func(x, y sessionInfo) int { return strings.Compare(x.ListKey, y.ListKey) })
	assert.Equal(t, []string{"agt_1", "agt_2", "agt_3", "agt_4"}, idsOf(byKey))
	slices.SortStableFunc(byKey, func(x, y sessionInfo) int { return strings.Compare(x.ListGroupKey, y.ListGroupKey) })
	assert.Equal(t, []string{"agt_4", "agt_1", "agt_2", "agt_3"}, idsOf(byKey))

	recs[0].UpdatedAt, recs[1].Status = t0.Add(time.Hour), statusRunning
	recs = append(recs, record{ID: "agt_5", Worktree: "/r/b", CreatedAt: t0.Add(2 * time.Second)}, record{ID: "agt_6", Worktree: "/r/c", CreatedAt: t0.Add(3 * time.Second)})
	after := keysOf(infosOf(recs))
	delete(after, "agt_5")
	delete(after, "agt_6")
	assert.True(t, maps.Equal(keysOf(before), after))
}

func TestSidebarKeepsTheNewestAndPicksTheNewest(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	recs := make([]record, 0, maxSidebarRows+2)
	for i := range maxSidebarRows + 2 {
		recs = append(recs, record{ID: fmt.Sprintf("agt_%04d", i), CreatedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	recs[len(recs)-1].Night = "sleep"
	window := sidebarWindow(recs)
	assert.Equal(t, []string{"agt_0002", "agt_1001"}, []string{window[0].ID, window[len(window)-1].ID})
	assert.Equal(t, "agt_1000", newestPick(window))
}

func idsOf(infos []sessionInfo) []string {
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.ID)
	}
	return out
}
