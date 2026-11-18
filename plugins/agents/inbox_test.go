package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

var inboxT0 = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func inboxAt(s int) time.Time { return inboxT0.Add(time.Duration(s) * time.Second) }

func inboxInfo(rec record, since int) sessionInfo {
	rec.CreatedAt, rec.UpdatedAt = inboxT0, inboxAt(100)
	info := sessionInfo{record: rec, NeedsYou: needsYou(rec)}
	if info.NeedsYou {
		at := inboxAt(since)
		info.NeedsYouSince = &at
	}
	return info
}

func inboxInfos(aSince int) []sessionInfo {
	infos := []sessionInfo{
		inboxInfo(record{ID: "agt_a", Worktree: orderHome, Status: statusWaiting}, aSince),
		inboxInfo(record{ID: "agt_b", Worktree: "/r/b", Status: statusWaiting}, 10),
		inboxInfo(record{ID: "agt_p", Worktree: orderHome, Status: statusRunning}, 0),
		inboxInfo(record{ID: "agt_k", Worktree: orderHome, lineage: lineage{ParentSession: "agt_p"}, Status: statusWaiting}, 5),
		inboxInfo(record{ID: "agt_o", Worktree: orderHome, Status: statusDone, OpenCount: 2, OpenSince: inboxAt(1)}, 0),
		inboxInfo(record{ID: "agt_r", Worktree: orderHome, Status: statusRunning}, 0),
		inboxInfo(record{ID: "agt_x", Worktree: orderHome, Status: statusClosed}, 0),
		runInfo("ws", runNeed{Run: "run_1", Title: "Nightly", Since: inboxAt(20)}),
	}
	setGroups(infos)
	return infos
}

func inboxKeys(infos []sessionInfo) map[string]string {
	keys := map[string]string{}
	for _, info := range infos {
		if info.InboxKey != "" {
			keys[info.ID] = info.InboxKey
		}
	}
	return keys
}

func TestInboxKeysPutNeedsYouBeforeOpenLongestWaitingFirst(t *testing.T) {
	t.Parallel()
	var memo inboxMemo
	infos := inboxInfos(30)
	memo.apply("ws", infos)
	var order []string
	for _, entry := range inboxOf(infos) {
		order = append(order, entry.ID)
	}
	assert.Equal(t, []string{"agt_p", "agt_b", "run_1", "agt_a", "agt_o"}, order)
	keys := inboxKeys(infos)
	assert.Len(t, keys, 5)
	assert.Equal(t, "0"+inboxAt(5).Format(startLayout)+" agt_p", keys["agt_p"])
	assert.Equal(t, "1"+inboxAt(1).Format(startLayout)+" agt_o", keys["agt_o"])
}

func TestInboxKeyHoldsWhileWaitingAndRenewsAfterLeaving(t *testing.T) {
	t.Parallel()
	var memo inboxMemo
	first := inboxInfos(30)
	memo.apply("ws", first)
	key := inboxKeys(first)["agt_a"]

	moved := inboxInfos(40)
	memo.apply("ws", moved)
	assert.Equal(t, key, inboxKeys(moved)["agt_a"])

	left := inboxInfos(40)
	left[0].Status, left[0].NeedsYou, left[0].NeedsYouSince = statusRunning, false, nil
	for i := range left {
		left[i].Group = ""
	}
	setGroups(left)
	memo.apply("ws", left)
	assert.NotContains(t, inboxKeys(left), "agt_a")

	back := inboxInfos(50)
	memo.apply("ws", back)
	assert.Equal(t, "0"+inboxAt(50).Format(startLayout)+" agt_a", inboxKeys(back)["agt_a"])
}

func TestInboxKeyRenewsForANeedThatLeftAndCameBack(t *testing.T) {
	t.Parallel()
	var memo inboxMemo
	memo.apply("ws", inboxInfos(30))
	other := []sessionInfo{inboxInfo(record{ID: "agt_w", Workspace: "other", Worktree: orderHome, Status: statusWaiting}, 5)}
	setGroups(other)
	memo.apply("other", other)

	gone := inboxInfos(30)
	memo.apply("ws", gone[:len(gone)-1])

	back := inboxInfos(30)
	back[len(back)-1] = runInfo("ws", runNeed{Run: "run_1", Title: "Nightly", Since: inboxAt(60)})
	setGroups(back)
	memo.apply("ws", back)
	assert.Equal(t, "0"+inboxAt(60).Format(startLayout)+" run_1", inboxKeys(back)["run_1"])

	again := []sessionInfo{inboxInfo(record{ID: "agt_w", Workspace: "other", Worktree: orderHome, Status: statusWaiting}, 70)}
	setGroups(again)
	memo.apply("other", again)
	assert.Equal(t, "0"+inboxAt(5).Format(startLayout)+" agt_w", inboxKeys(again)["agt_w"])
}

func TestSidebarInboxMirrorsSessionsOnTop(t *testing.T) {
	t.Parallel()
	var memo inboxMemo
	infos := inboxInfos(30)
	memo.apply("ws", infos)
	var recs []record
	for _, info := range infos {
		if info.Kind == "" {
			recs = append(recs, info.record)
		}
	}
	children := childStatuses(recs)
	var rows []sessionRow
	for _, rec := range recs {
		row := sessionRow{Rec: rec, Children: children[rec.ID]}
		if rec.Worktree == "/r/b" {
			row.Branch = "chore/awake-mac-shots"
		}
		rows = append(rows, row)
	}
	rows = append(rows, sessionRow{Rec: record{ID: "agt_n", Worktree: orderHome, Status: statusDone, CreatedAt: inboxT0, Run: &api.Run{ID: "run_1", Title: "Nightly"}}})
	view := func(filter string, inbox []inboxEntry) sdui.Node {
		return sidebarView(rows, "agt_b", nil, inboxAt(90), orderHome, map[string]string{orderHome: homeListGroup, "/r/b": "1"}, false, filter, sidebarSearch{}, 0, sidebarPlus{}, inbox)
	}
	tree := nodesOf(view(filterAll, inboxOf(infos)), "tree")[0]
	assert.Equal(t, filterAll, tree.Props["hold"])
	items := tree.Props["items"].([]sdui.TreeItem)
	require.GreaterOrEqual(t, len(items), 3)
	needs, open := items[0], items[1]
	assert.Equal(t, groupPrefix+inboxNeedsKey, needs.ID)
	assert.Equal(t, "4", needs.Detail)
	var mirrored []string
	for _, item := range needs.Children {
		mirrored = append(mirrored, item.MirrorOf)
		assert.Equal(t, mirrorPrefix+item.MirrorOf, item.ID)
		assert.Empty(t, item.Children)
	}
	assert.Equal(t, []string{"agt_p", "agt_b", runPrefix + "run_1", "agt_a"}, mirrored)
	assert.Equal(t, "chore · 1 min", needs.Children[1].Detail)
	assert.Equal(t, groupPrefix+inboxOpenKey, open.ID)
	require.Len(t, open.Children, 1)
	assert.Equal(t, "agt_o", open.Children[0].MirrorOf)
	assert.Equal(t, groupPrefix+orderHome, items[2].ID)
	assert.NotNil(t, findItem(items[2:], "agt_a"))
	assert.NotContains(t, shownSessions(items), mirrorPrefix+"agt_b")

	needsOnly := nodesOf(view(filterNeeds, inboxOf(infos)), "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Nil(t, findItem(needsOnly, groupPrefix+inboxNeedsKey))

	flat := nodesOf(view(filterAll, nil), "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Nil(t, findItem(flat, groupPrefix+inboxNeedsKey))
}

func TestSidebarInboxCopiesARunOnce(t *testing.T) {
	t.Parallel()
	run := &api.Run{ID: "run_1", Title: "Nightly"}
	recs := []record{
		{ID: "agt_s1", Worktree: orderHome, Status: statusWaiting, Run: run},
		{ID: "agt_s2", Worktree: orderHome, Status: statusWaiting, Run: run},
		{ID: "agt_s3", Worktree: orderHome, Status: statusDone, OpenCount: 1, OpenSince: inboxAt(1), Run: run},
	}
	infos := make([]sessionInfo, 0, len(recs)+1)
	rows := make([]sessionRow, 0, len(recs))
	for i, rec := range recs {
		infos = append(infos, inboxInfo(rec, 10+i))
		rows = append(rows, sessionRow{Rec: infos[i].record})
	}
	infos = append(infos, runInfo("ws", runNeed{Run: "run_1", Title: "Nightly", Since: inboxAt(5)}))
	setGroups(infos)
	var memo inboxMemo
	memo.apply("ws", infos)
	tree := nodesOf(sidebarView(rows, "", nil, inboxAt(90), orderHome, map[string]string{orderHome: homeListGroup}, false, filterAll, sidebarSearch{}, 0, sidebarPlus{}, inboxOf(infos)), "tree")[0]
	require.NoError(t, internalsdui.Validate(tree))
	items := tree.Props["items"].([]sdui.TreeItem)
	require.Equal(t, groupPrefix+inboxNeedsKey, items[0].ID)
	require.Len(t, items[0].Children, 1)
	assert.Equal(t, runPrefix+"run_1", items[0].Children[0].MirrorOf)
	assert.Nil(t, findItem(items, groupPrefix+inboxOpenKey))
}

func TestSidebarInboxDrawsAGateWaitingForTrustOnOneLine(t *testing.T) {
	t.Parallel()
	held := inboxInfo(record{ID: "agt_h", Worktree: orderHome, Status: statusRunning}, 0)
	infos := []sessionInfo{held, trustInfo("ws", trustNeed{Gate: "gt_1", Name: gateNameDone + "plans and specs", Point: gatePointTurn, Waits: 2, Since: inboxAt(5)})}
	setGroups(infos)
	var memo inboxMemo
	memo.apply("ws", infos)
	tree := nodesOf(sidebarView([]sessionRow{{Rec: held.record}}, "", nil, inboxAt(90), orderHome, map[string]string{orderHome: homeListGroup}, false, filterAll, sidebarSearch{}, 0, sidebarPlus{}, inboxOf(infos)), "tree")[0]
	require.NoError(t, internalsdui.Validate(tree))

	got := tree.Props["items"].([]sdui.TreeItem)[0].Children

	assert.Equal(t, []sdui.TreeItem{{
		ID: mirrorPrefix + gatePrefix + "gt_1", MirrorOf: gatePrefix + "gt_1", Label: "plans and specs", Icon: gateDoneIcon, Tone: toneWarning,
		Badges: []sdui.HeaderBadge{{Text: "to trust", Tone: toneWarning}}, Help: "Graph changed · 2 waits",
	}}, got)
}
