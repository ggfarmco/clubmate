package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
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

func TestListFiltersAddUpToTheMacBadges(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recs := []record{
		{ID: "agt_m1", Worktree: orderHome, Status: statusWaiting, CreatedAt: at(1)},
		{ID: "agt_m2", Worktree: orderHome, Status: statusRunning, CreatedAt: at(2)},
		{ID: "agt_m3", Worktree: orderHome, Status: statusDone, OpenCount: 2, CreatedAt: at(3)},
		{ID: "agt_p", Worktree: "/r/b", Status: statusDone, CreatedAt: at(4)},
		{ID: "agt_k", Worktree: "/r/b", lineage: lineage{ParentSession: "agt_p"}, Status: statusRunning, CreatedAt: at(5)},
		{ID: "agt_s", Worktree: orderHome, Night: "deps", Status: statusWaiting, CreatedAt: at(6)},
		{ID: "agt_x", Worktree: orderHome, Status: statusClosed, OpenCount: 1, CreatedAt: at(7)},
		{ID: "agt_g", Worktree: "/r/b", Run: &api.Run{ID: "run_1"}, Status: statusDone, CreatedAt: at(8)},
		{ID: "agt_l", Worktree: "/r/c", Status: statusDone, CreatedAt: at(10)},
		{ID: "agt_r", Worktree: orderHome, Status: statusRunning, CreatedAt: at(12)},
		{ID: "agt_rk", Worktree: orderHome, lineage: lineage{ParentSession: "agt_r"}, Status: statusWaiting, CreatedAt: at(13)},
		{ID: "agt_e", Worktree: orderHome, Status: statusError, CreatedAt: at(14)},
		{ID: "agt_ek", Worktree: orderHome, lineage: lineage{ParentSession: "agt_e"}, Status: statusWaiting, CreatedAt: at(15)},
		{ID: "agt_c", Worktree: orderHome, Status: statusClosed, CreatedAt: at(16)},
		{ID: "agt_ck", Worktree: orderHome, lineage: lineage{ParentSession: "agt_c"}, Status: statusWaiting, CreatedAt: at(17)},
	}
	needs := map[string]runNeed{
		"run_1": {Run: "run_1", Kind: needStep, Request: "step-1", Since: at(9)},
		"run_g": {Run: "run_g", Kind: needStep, Request: "step-2", Since: at(11)},
		"run_o": {Run: "run_o", Kind: needStep, Request: "step-3", Since: at(18)},
	}
	landings := map[string]*landingView{"agt_l": {Run: "run_g"}}
	children := childStatuses(recs)
	infos := make([]sessionInfo, 0, len(recs)+2)
	rows := make([]sessionRow, 0, len(recs))
	for _, rec := range recs {
		infos = append(infos, sessionInfo{record: rec, ChildStatus: children[rec.ID], Landing: landings[rec.ID]})
		row := sessionRow{Rec: rec, Children: children[rec.ID], Landing: landings[rec.ID]}
		if rec.Run != nil {
			row.RunNeed = new(needs["run_1"])
		}
		if l := landings[rec.ID]; l != nil {
			row.GateAsk = new(needs[l.Run])
		}
		rows = append(rows, row)
	}
	infos = append(infos, runInfo("w", needs["run_1"]), runInfo("w", needs["run_g"]), runInfo("w", needs["run_o"]), trustInfo("w", trustNeed{Gate: "gt_1", Name: "Done · plans and specs", Waits: 2, Since: at(19)}))
	setListFilters(infos, needs)

	phone := map[string]int{}
	for _, info := range infos {
		for _, f := range info.ListFilters {
			if f != filterOpen {
				phone[f]++
			}
		}
		phone[filterOpen] += info.ListOpen
	}
	tree := sidebarView(rows, "", map[string]bool{}, t0, orderHome, groupKeys(recs, orderHome), false, "", sidebarSearch{}, 0, sidebarPlus{needRuns: []string{"run_1", "run_g", "run_o", "gt_1"}}, nil)
	mac := map[string]int{}
	for _, item := range nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item) {
		var n int
		fmt.Sscan(item.Badge, &n)
		mac[item.ID] = n
	}
	assert.Equal(t, map[string]int{filterAll: 8, filterNeeds: 9, filterOpen: 2, filterRunning: 3}, mac)
	assert.Equal(t, mac, phone)
	assert.Equal(t, []string{"agt_m1", "agt_s", "agt_l", "agt_r", "agt_e", "agt_c", "run_1", "run_o", "gt_1"}, idsOf(slices.DeleteFunc(slices.Clone(infos), func(info sessionInfo) bool {
		return !slices.Contains(info.ListFilters, filterNeeds)
	})))
}

func TestGroupLabelsNameTheBranchAndRunsSitWithTheirFirstNode(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	infos := []sessionInfo{
		{record: record{ID: "agt_m", Worktree: orderHome, CreatedAt: t0}},
		{record: record{ID: "agt_g2", Worktree: "/r/wt/feat-x", Run: &api.Run{ID: "run_1"}, CreatedAt: t0.Add(2 * time.Second)}},
		{record: record{ID: "agt_g1", Worktree: "/r/wt/feat-x", Run: &api.Run{ID: "run_1"}, CreatedAt: t0.Add(time.Second)}},
		runInfo("w", runNeed{Run: "run_1", Since: t0.Add(time.Hour)}),
	}
	setOrder(infos, orderHome)
	setGroupLabels(infos, func(worktree string) string {
		if worktree == orderHome {
			return "main"
		}
		return ""
	})
	assert.Equal(t, []string{"main", "feat-x", "feat-x", "feat-x"}, []string{infos[0].ListGroupLabel, infos[1].ListGroupLabel, infos[2].ListGroupLabel, infos[3].ListGroupLabel})
	assert.Equal(t, infos[2].ListKey, infos[3].ListKey)
	assert.Equal(t, infos[2].ListGroupKey, infos[3].ListGroupKey)
}

func TestSessionsSinceFailsWhenTheWorkspaceCannotBeRead(t *testing.T) {
	h := scriptedHost(t, func(method, _ string) any {
		if method == "workspace.get" {
			return "unreadable"
		}
		return nil
	})
	a := &app{host: h}
	since := ""

	_, err := a.sessionsCommand(t.Context(), pluginsdk.CommandRun{WorkspaceID: "prj_1"}, sessionsArgs{Since: &since})
	assert.Error(t, err)
}
