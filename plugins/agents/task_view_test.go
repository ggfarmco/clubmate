package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func taskRows(now time.Time) []sessionRow {
	task := &taskRecord{Slug: "pause", Repos: []taskRepo{
		{Path: "/r/snapshot", Name: "ads-snapshot", Order: 1, State: landMerged, Session: "agt_a"},
		{Path: "/r/auth", Name: "internal-auth", Order: 2, State: landFailed, Session: "agt_b"},
		{Path: "/r/ads", Name: "ads-management", Order: 3, State: landRunning, Session: "agt_c"},
		{Path: "/r/web", Name: "web", Order: 4, State: landAsking, Session: "agt_d"},
		{Path: "/r/bff", Name: "mobile-bff", Order: 5, State: landWaits},
		{Path: "/r/etl", Name: "analytics-etl", Order: 6, State: landWaits},
	}}
	owner := record{ID: "agt_o", Worktree: "/r/a", Title: "Campaign pause reason", Status: statusDone, Task: task, ConnectRequests: []connectRequest{
		{ID: "cr_1", Repos: []connectRepo{{Path: "/r/etl", Name: "analytics-etl", State: repoPending}, {Path: "/r/new", Name: "new-repo", State: repoPending}}},
	}}
	kid := func(id, repo, status string) record {
		return record{ID: id, Worktree: repo, Title: "Long task title of " + id, Status: status, ParentSession: "agt_o", RepoPath: repo}
	}
	return []sessionRow{
		{Rec: owner},
		{Rec: kid("agt_d", "/r/web", statusWaiting)},
		{Rec: kid("agt_c", "/r/ads", statusRunning), Started: now.Add(-4 * time.Minute)},
		{Rec: kid("agt_b", "/r/auth", statusError)},
		{Rec: kid("agt_a", "/r/snapshot", statusDone)},
		{Rec: record{ID: "agt_x", Worktree: "/r/a", Title: "Helper", Status: statusDone, ParentSession: "agt_o"}},
	}
}

func sessionsOf(t *testing.T, tree sdui.Node) []sdui.TreeItem {
	t.Helper()
	require.NoError(t, internalsdui.Validate(tree))
	return nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
}

func TestTaskTreeNamesReposInLandingOrder(t *testing.T) {
	t.Parallel()
	now := time.Now()
	items := sessionsOf(t, sidebarView(taskRows(now), "", nil, now, "/r/a", false, "", sidebarSearch{}, 0))
	require.Len(t, items, 1)
	parent := items[0]
	assert.Equal(t, "4 need you", parent.Detail)
	assert.Equal(t, "warning", parent.DetailTone)
	assert.Equal(t, statusIcons[statusWaiting], parent.Icon)
	assert.True(t, parent.Expanded)

	type shown struct{ label, word, tone string }
	var got []shown
	for _, item := range parent.Children {
		got = append(got, shown{item.Label, item.Detail, item.Tone})
		assert.LessOrEqual(t, len([]rune(item.Detail)), 9, item.Label)
	}
	assert.Equal(t, []shown{
		{"ads-snapshot", wordMerged, "success"},
		{"internal-auth", wordFailed, "danger"},
		{"ads-management", "4 min", "accent"},
		{"web", wordAsking, "warning"},
		{"mobile-bff", wordWaits, ""},
		{"analytics-etl", wordConnect, "warning"},
		{"new-repo", wordConnect, "warning"},
	}, got[:7])
	assert.Equal(t, "Helper", got[7].label)
	assert.Equal(t, repoIcon, parent.Children[0].LabelIcon)
	assert.Empty(t, parent.Children[7].LabelIcon)

	assert.Equal(t, "agt_b", parent.Children[1].ID)
	owner, ok := repoOwner(parent.Children[6].ID)
	require.True(t, ok)
	assert.Equal(t, "agt_o", owner)
	assert.True(t, parent.Children[6].Action)
}

func TestConnectRowIsNeutralWhileTheTurnRuns(t *testing.T) {
	t.Parallel()
	rows := []sessionRow{{Rec: record{ID: "agt_o", Worktree: "/r/a", Status: statusRunning, ConnectRequests: []connectRequest{
		{ID: "cr_1", Repos: []connectRepo{{Path: "/r/new", Name: "new-repo", State: repoPending}}},
	}}}}
	items := sessionsOf(t, sidebarView(rows, "", nil, time.Now(), "/r/a", false, "", sidebarSearch{}, 0))
	require.Len(t, items[0].Children, 1)
	assert.Equal(t, wordConnect, items[0].Children[0].Detail)
	assert.Empty(t, items[0].Children[0].Tone)
	assert.Empty(t, items[0].DetailTone)
}

func TestRepoSummary(t *testing.T) {
	t.Parallel()
	task := func(states ...string) *taskRecord {
		out := &taskRecord{}
		for _, state := range states {
			out.Repos = append(out.Repos, taskRepo{State: state})
		}
		return out
	}
	waiting := []record{{Status: statusWaiting, ParentSession: "agt_o"}}
	for _, c := range []struct {
		name   string
		rec    record
		kids   []record
		want   string
		urgent bool
	}{
		{"one failed", record{Task: task(landFailed, landWaits)}, nil, "1 failed", true},
		{"failed and a waiting child", record{Task: task(landFailed, landAsking)}, waiting, "2 need you", true},
		{"one asking", record{Task: task(landAsking, landWaits)}, waiting, "1 needs you", true},
		{"nothing needs you", record{Task: task(landWaits, landWaits, landMerged)}, nil, "3 repos", false},
		{"one repo", record{Task: task(landWaits)}, nil, "1 repo", false},
		{"land request", record{Task: &taskRecord{Repos: []taskRepo{{Request: "lr_1"}, {Request: "lr_2"}}}}, nil, "2 need you", true},
		{"no task", record{}, nil, "", false},
	} {
		got, urgent := repoSummary(c.rec, c.kids)
		assert.Equal(t, c.want, got, c.name)
		assert.Equal(t, c.urgent, urgent, c.name)
	}
}

func TestFoldedTaskKeepsPinnedChildren(t *testing.T) {
	t.Parallel()
	now := time.Now()
	collapsed := map[string]bool{expandPrefix + "agt_o": true}
	items := sessionsOf(t, sidebarView(taskRows(now), "", collapsed, now, "/r/a", false, "", sidebarSearch{}, 0))
	parent := items[0]
	assert.False(t, parent.Expanded)
	var labels []string
	for _, item := range parent.Children {
		assert.True(t, item.Pinned)
		labels = append(labels, item.Label)
	}
	assert.Equal(t, []string{"internal-auth", "web", "analytics-etl", "new-repo"}, labels)
}

func TestStepSkipsRowsWithNoSession(t *testing.T) {
	t.Parallel()
	now := time.Now()
	collapsed := map[string]bool{expandPrefix + "agt_o": true}
	raw, err := json.Marshal(sidebarView(taskRows(now), "agt_d", collapsed, now, "/r/a", false, "", sidebarSearch{}, 0))
	require.NoError(t, err)
	assert.Equal(t, "agt_o", stepSession(raw, 1))
	assert.Equal(t, "agt_b", stepSession(raw, -1))
}

func TestCrumbCarriesThePositionAndTheKeys(t *testing.T) {
	t.Parallel()
	node := crumbNode("agt_c", repoCrumb{Parent: "Campaign", Repo: "ads", At: 3, Of: 8, Prev: "agt_b"})
	require.NoError(t, internalsdui.Validate(sdui.VStack(node)))
	assert.Equal(t, "3 of 8", node.Props["note"])
	assert.Equal(t, prevKey, node.Children[0].Props["shortcut"])
	assert.NotEqual(t, true, node.Children[0].Props["disabled"])
	assert.Equal(t, true, node.Children[1].Props["disabled"])

	sc := screen{Crumb: &repoCrumb{Waits: "waits for internal-auth"}}
	assert.Equal(t, []sdui.HeaderBadge{{Text: "waits for internal-auth"}}, nodesOf(header(sc), "header")[0].Props["badges"])
}

func TestAFKRow(t *testing.T) {
	t.Parallel()
	en := &entry{At: time.Date(2026, 11, 6, 2, 14, 0, 0, time.Local)}
	n := noticeUpdate{Repo: "internal-auth", Path: "/r/auth"}
	sc := screen{Workspace: "ads", Folders: pluginsdk.Workspace{Folders: []pluginsdk.Folder{{Path: "/r/auth"}}}}
	sc.View.Session = record{ID: "agt_o", Task: &taskRecord{Repos: []taskRepo{{Path: "/r/auth", State: landPROpen}}}}

	row := afkRow(sc, en, n)
	require.NoError(t, internalsdui.Validate(sdui.VStack(row)))
	assert.Equal(t, "02:14 · "+afkPROpened, textsOf(row)[0])
	buttons := nodesOf(row, "button")
	require.Len(t, buttons, 1)
	assert.Equal(t, "Remove from ads…", buttons[0].Props["label"])
	assert.Equal(t, "agt_o/folderRemove//r/auth", buttons[0].ID)

	sc.Folders = pluginsdk.Workspace{}
	gone := afkRow(sc, en, n)
	assert.Empty(t, nodesOf(gone, "button"))
	assert.Equal(t, "02:14 · removed from ads", textsOf(gone)[0])
}

func TestMidOfKeepsBothEndsOfALongBranch(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "feat/x", midOf("feat/x", folderBranchWidth))
	assert.Equal(t, "feat/a…reason", midOf("feat/abcdefghij-reason", 13))
}

func textsOf(n sdui.Node) []string {
	var out []string
	for _, text := range nodesOf(n, "text") {
		if s, ok := text.Props["text"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestFolderSheetView(t *testing.T) {
	t.Parallel()
	sheet := &folderSheet{ticked: map[string]bool{}, impact: folderImpactResult{
		Name: "internal-auth", Workspace: "ads", Path: "/r/auth",
		Worktrees: []folderWorktree{{Path: "/r/auth/.worktrees/x", Branch: "feat/x`y", Read: true, Dirty: 1, Unpushed: 2}, {Path: "/r/auth/.worktrees/z", Branch: "z", Read: true, Unread: true}, {Path: "/r/auth/.worktrees/w", Branch: "w"}},
		Graphs:    []string{"scope-lint"}, Hooks: []string{"setup.sh", "teardown.sh"}, Instructions: []string{"CLAUDE.md"}, Skills: []string{"a", "b"},
		TrustedGraphs: []string{"scope-lint"}, TrustedHooks: true, CronJobs: []folderJob{{ID: "cj_1", Name: "night*ly"}},
	}}
	holds := folderHolds{sessions: []folderSession{{Title: "t", Status: statusClosed}}}

	view := sheet.view(holds)
	require.NoError(t, internalsdui.Validate(sdui.VStack(view)))
	assert.Equal(t, "Remove internal-auth from ads?", view.Props["title"])
	assert.Equal(t, folderRefused, view.Props["note"])
	rows, _ := nodesOf(view, "impact")[0].Props["rows"].([]sdui.ImpactRow)
	titles := make([]string, 0, len(rows))
	for _, r := range rows {
		titles = append(titles, r.Title)
	}
	assert.Equal(t, []string{"Sessions", "Worktrees", "Graphs", "Worktree hooks", "Agent instructions", "Kept", "Cron jobs", "Untouched"}, titles)
	assert.Len(t, nodesOf(view, "toggle"), 1)
	buttons := nodesOf(view, "button")
	assert.Equal(t, "Remove from ads", buttons[1].Props["label"])
	assert.Equal(t, sdui.VariantPrimary, buttons[1].Props["variant"])
	assert.Equal(t, "cmd+return", buttons[1].Props["shortcut"])

	sheet.ticked["/r/auth/.worktrees/x"] = true
	buttons = nodesOf(sheet.view(holds), "button")
	assert.Equal(t, "Remove · lose 1 file", buttons[1].Props["label"])
	assert.Equal(t, sdui.VariantDanger, buttons[1].Props["variant"])

	held := holds
	held.blockers = []folderSession{{Title: "Fix auth", Status: statusRunning}}
	refused := sheet.view(held)
	assert.Equal(t, "Refused: 1 session works there.", refused.Props["note"])
	assert.Equal(t, true, nodesOf(refused, "button")[1].Props["disabled"])
	assert.Contains(t, textsOf(refused), "Fix auth is running")
}
