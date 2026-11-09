package main

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClassifyWorktree(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	session := &worktreeSessionRef{ID: "s"}
	task := &worktreeTask{Title: "t"}
	tests := []struct {
		name  string
		row   worktreeRow
		hand  bool
		stale bool
		group string
	}{
		{name: "idle one second short of 30 days", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Committed: ago(30*day - time.Second)}}, group: groupOther},
		{name: "idle exactly 30 days", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Committed: ago(30 * day)}}, stale: true, group: groupStale},
		{name: "opening resets idle", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Committed: ago(90 * day), LastOpened: ago(10 * day)}}, group: groupOther},
		{name: "made recently from an old base is not idle", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Committed: ago(40 * day), Created: ago(day)}}, group: groupOther},
		{name: "session activity resets idle", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Committed: ago(40 * day)}, Active: ago(day)}, group: groupOther},
		{name: "merged is stale at once", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Merged: true, Committed: ago(day)}}, stale: true, group: groupStale},
		{name: "live is never stale", row: worktreeRow{gitWorktree: gitWorktree{MadeBy: "agents", Merged: true, Committed: ago(90 * day)}, Live: true, Session: session}, group: groupLive},
		{name: "current is never stale", row: worktreeRow{Now: true, gitWorktree: gitWorktree{Current: true, Merged: true, Committed: ago(90 * day)}, Session: session}, group: groupNow},
		{name: "the main checkout is never stale or by hand", row: worktreeRow{gitWorktree: gitWorktree{Main: true, Committed: ago(90 * day)}}, group: groupOther},
		{name: "no madeBy and no session is by hand", row: worktreeRow{gitWorktree: gitWorktree{Committed: ago(day)}}, hand: true, group: groupHand},
		{name: "by hand beats stale", row: worktreeRow{gitWorktree: gitWorktree{Committed: ago(90 * day)}}, hand: true, stale: true, group: groupHand},
		{name: "a session record is not by hand", row: worktreeRow{gitWorktree: gitWorktree{Committed: ago(day)}, Session: session, Task: task}, group: groupTask},
		{name: "stale beats task, so Stale counts every merged worktree", row: worktreeRow{gitWorktree: gitWorktree{Merged: true, Committed: ago(day)}, Session: session, Task: task}, stale: true, group: groupStale},
		{name: "opened within 7 days is recent", row: worktreeRow{gitWorktree: gitWorktree{Main: true, LastOpened: ago(7 * day)}}, group: groupRecent},
		{name: "opened over 7 days ago is not", row: worktreeRow{gitWorktree: gitWorktree{Main: true, LastOpened: ago(7*day + time.Second)}}, group: groupOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := tt.row
			row.classify(now)

			assert.Equal(t, tt.hand, row.ByHand, "byHand")
			assert.Equal(t, tt.stale, row.Stale, "stale")
			assert.Equal(t, tt.group, row.Group, "group")
		})
	}
}

func TestJoinWorktreesKeepsTheLatestSessionActivity(t *testing.T) {
	t.Parallel()
	now := time.Now()
	index := []gitWorktree{{Repo: "/r", Path: "/r/wt", Committed: now.Add(-90 * 24 * time.Hour)}}
	older := now.Add(-40 * 24 * time.Hour)
	latest := now.Add(-time.Hour)
	facts := []sessionFact{
		{rec: record{ID: "closed", Worktree: "/r/wt", Status: statusClosed, UpdatedAt: latest}},
		{rec: record{ID: "open", Worktree: "/r/wt", Status: statusIdle, UpdatedAt: older}},
	}

	rows, _ := joinWorktrees(index, facts, "", now)

	assert.Equal(t, latest, rows[0].Active)
	assert.False(t, rows[0].Stale)
}

func TestJoinWorktreesPicksTheLiveSession(t *testing.T) {
	t.Parallel()
	now := time.Now()
	index := []gitWorktree{{Repo: "/r", Path: "/r", Main: true}, {Repo: "/r", Path: "/r/.worktrees/a", MadeBy: "agents"}}
	facts := []sessionFact{
		{rec: record{ID: "old", Worktree: "/r/.worktrees/a", Title: "Old", Status: statusDone, UpdatedAt: now}},
		{rec: record{ID: "busy", Worktree: "/r/.worktrees/a/sub", Title: "Busy", Status: statusRunning, UpdatedAt: now.Add(-time.Hour)}, live: true},
	}

	rows, tasks := joinWorktrees(index, facts, "", now)

	assert.Empty(t, tasks)
	assert.Nil(t, rows[1].Session)
	got := rows[0]
	assert.Equal(t, "/r/.worktrees/a", got.Path)
	assert.True(t, got.Live)
	assert.Equal(t, worktreeSessionRef{ID: "busy", Title: "Busy", Status: statusRunning}, *got.Session)
	assert.Equal(t, worktreeTask{Title: "Busy", State: statusRunning}, *got.Task)
}

func TestJoinWorktreesSaysWhatAClosedSessionLeft(t *testing.T) {
	t.Parallel()
	now := time.Now()
	index := []gitWorktree{
		{Repo: "/r", Path: "/r/.worktrees/a", Committed: now, Merged: true},
		{Repo: "/r", Path: "/r/.worktrees/b", Committed: now},
	}
	facts := []sessionFact{
		{rec: record{ID: "a", Worktree: "/r/.worktrees/a", Title: "A", Status: statusClosed}},
		{rec: record{ID: "b", Worktree: "/r/.worktrees/b", Title: "B", Status: statusClosed}},
	}

	rows, _ := joinWorktrees(index, facts, "", now)

	byPath := map[string]string{}
	for _, row := range rows {
		byPath[row.Path] = row.Task.State
	}
	assert.Equal(t, wordMerged, byPath["/r/.worktrees/a"])
	assert.Equal(t, wordOnBranch, byPath["/r/.worktrees/b"])
}

func TestJoinWorktreesTaskIsOneRowAcrossFolders(t *testing.T) {
	t.Parallel()
	now := time.Now()
	index := []gitWorktree{
		{Repo: "/ads", Path: "/ads/.worktrees/pause", Branch: "feat/pause", Committed: now},
		{Repo: "/auth", Path: "/auth/.worktrees/pause", Branch: "feat/pause", Committed: now},
		{Repo: "/auth", Path: "/auth/.worktrees/other", Branch: "other", Committed: now},
	}
	owner := record{ID: "boss", Title: "Pause reason", Status: statusDone, Task: &taskRecord{Slug: "pause", Repos: []taskRepo{
		{Path: "/ads", Name: "ads", Session: "kid1", State: landMerged},
		{Path: "/auth", Name: "auth", Session: "kid2", State: landFailed},
		{Path: "/snap", Name: "snap", State: landWaits},
	}}}
	facts := []sessionFact{
		{rec: owner},
		{rec: record{ID: "kid1", Worktree: "/ads/.worktrees/pause", Title: "ads work", Status: statusDone}},
		{rec: record{ID: "kid2", Worktree: "/auth/.worktrees/pause", Title: "auth work", Status: statusRunning}, live: true},
	}

	rows, tasks := joinWorktrees(index, facts, "", now)

	assert.Len(t, tasks, 1)
	row := tasks[0]
	assert.Equal(t, "boss", row.Session)
	assert.Equal(t, "Pause reason", row.Title)
	assert.Equal(t, "3 folders · 1 merged · 1 failed · 1 waits", row.State)
	assert.Equal(t, []string{"/ads", "/auth", "/snap"}, row.Folders)
	assert.ElementsMatch(t, []string{"/ads/.worktrees/pause", "/auth/.worktrees/pause"}, row.Worktrees)
	assert.True(t, row.Live)
	assert.Equal(t, groupLive, row.Group)
	words := map[string]worktreeTask{}
	for _, r := range rows {
		if r.Task != nil {
			words[r.Path] = *r.Task
		}
	}
	assert.Equal(t, worktreeTask{Session: "boss", Title: "Pause reason", State: wordMerged}, words["/ads/.worktrees/pause"])
	assert.Equal(t, worktreeTask{Session: "boss", Title: "Pause reason", State: wordFailed}, words["/auth/.worktrees/pause"])
}

func TestJoinWorktreesDropsAClosedTaskWithNothingLeft(t *testing.T) {
	t.Parallel()
	facts := []sessionFact{{rec: record{ID: "boss", Status: statusClosed, Task: &taskRecord{Slug: "pause", Repos: []taskRepo{{Path: "/a", Session: "gone"}}}}}}

	_, tasks := joinWorktrees(nil, facts, "", time.Now())

	assert.Empty(t, tasks)
}

func TestJoinWorktreesLeavesAGoneWorktreeOffTheMainCheckout(t *testing.T) {
	t.Parallel()
	index := []gitWorktree{{Repo: "/r", Path: "/r", Main: true}}
	facts := []sessionFact{
		{rec: record{ID: "boss", Status: statusClosed, Task: &taskRecord{Slug: "pause", Repos: []taskRepo{{Path: "/r", Session: "kid"}}}}},
		{rec: record{ID: "kid", Worktree: "/r/.worktrees/gone", Status: statusClosed}},
	}

	rows, tasks := joinWorktrees(index, facts, "", time.Now())

	assert.Nil(t, rows[0].Session)
	assert.Empty(t, tasks)
}

func TestJoinWorktreesOrdersLiveFirstAndStaleLast(t *testing.T) {
	t.Parallel()
	now := time.Now()
	old := now.Add(-90 * 24 * time.Hour)
	index := []gitWorktree{
		{Repo: "/r", Path: "/stale", MadeBy: "agents", Committed: old},
		{Repo: "/r", Path: "/hand", Committed: now},
		{Repo: "/r", Path: "/rest", MadeBy: "agents", Committed: now},
		{Repo: "/r", Path: "/recent", MadeBy: "agents", LastOpened: now},
		{Repo: "/r", Path: "/live", MadeBy: "agents", Committed: old},
	}
	facts := []sessionFact{{rec: record{ID: "s", Worktree: "/live", Status: statusRunning}, live: true}}

	rows, _ := joinWorktrees(index, facts, "", now)

	var paths []string
	for _, row := range rows {
		paths = append(paths, row.Path)
	}
	assert.Equal(t, []string{"/live", "/recent", "/rest", "/hand", "/stale"}, paths)
}

func pickerRows(repo string, group string, count int) []worktreeRow {
	rows := make([]worktreeRow, count)
	for i := range rows {
		rows[i] = worktreeRow{gitWorktree: gitWorktree{Repo: repo, Path: fmt.Sprintf("%s/%s-%d", repo, group, i)}, Group: group}
	}
	return rows
}

func TestBuildPickerKeepsAFewWorktreesByFolder(t *testing.T) {
	t.Parallel()
	rows := []worktreeRow{
		{gitWorktree: gitWorktree{Repo: "/a", Path: "/a/x"}, Group: groupLive},
		{gitWorktree: gitWorktree{Repo: "/a", Path: "/a", Main: true}, Group: groupOther},
	}
	folders := []worktreeFolder{{Path: "/a", Name: "a", Worktrees: 2, Live: 1}}

	got := buildPicker("ads", folders, rows)

	assert.True(t, got.ByFolder)
	assert.Equal(t, pickerOneFolder, got.Placeholder)
	assert.Empty(t, got.Count)
	assert.Equal(t, []int{1, 0}, folders[0].Rows, "the main checkout leads")
	assert.Equal(t, "2 worktrees · 1 live", folders[0].Summary)
	assert.Equal(t, "Add a folder to ads…", got.AddFolder)
}

func TestBuildPickerCapsAboveTwentyWorktrees(t *testing.T) {
	t.Parallel()
	rows := slices.Concat(
		pickerRows("/a", groupNow, 1),
		pickerRows("/a", groupLive, 9),
		pickerRows("/b", groupRecent, 8),
		pickerRows("/a", groupTask, 5),
		pickerRows("/a", groupHand, 3),
		pickerRows("/b", groupStale, 4),
	)
	folders := []worktreeFolder{{Path: "/a", Name: "a", Worktrees: 18}, {Path: "/b", Name: "b", Worktrees: 12}}

	got := buildPicker("ads", folders, rows)

	assert.False(t, got.ByFolder)
	assert.Equal(t, "2 folders · 30 worktrees", got.Count)
	ids := func(sections []pickerSection) []string {
		out := []string{}
		for _, s := range sections {
			out = append(out, s.ID)
		}
		return out
	}
	assert.Equal(t, []string{groupNow, groupLive, groupRecent, "folders"}, ids(got.Sections))
	assert.Equal(t, "Live · 9", got.Sections[1].Title, "live is never capped")
	assert.Len(t, got.Sections[1].Rows, 9)
	assert.Len(t, got.Sections[2].Rows, pickerRecentMax)

	home := folders[0].Sections
	assert.Equal(t, []string{groupLive, groupRecent, groupTask, groupHand}, ids(home))
	assert.Equal(t, []int{0}, home[1].Rows, "the row Code shows sits in its folder's Recent")
	task := home[2]
	assert.Equal(t, pickerTaskShown, task.Shown)
	assert.Equal(t, "3 more · type to find one", task.More)
	hand := home[3]
	assert.Zero(t, hand.Shown)
	assert.Equal(t, "show 3", hand.Fold)
	assert.Empty(t, hand.More)
}

func TestJoinWorktreesNowIsTheRowOfTheFolderCodeShows(t *testing.T) {
	t.Parallel()
	now := time.Now()
	index := []gitWorktree{
		{Repo: "/a", Path: "/a", Main: true, Current: true, Committed: now},
		{Repo: "/b", Path: "/b", Main: true, Current: true, Committed: now},
	}

	rows, _ := joinWorktrees(index, nil, "/b", now)
	byRepo := map[string]worktreeRow{rows[0].Repo: rows[0], rows[1].Repo: rows[1]}
	assert.True(t, byRepo["/b"].Now)
	assert.False(t, byRepo["/a"].Now)
	assert.Equal(t, groupOther, byRepo["/a"].Group)

	rows, _ = joinWorktrees(index, nil, "", now)
	assert.Equal(t, "/a", rows[0].Repo, "with no editor to ask, Code is on the first folder")
	assert.True(t, rows[0].Now)
}

func TestDescribeWords(t *testing.T) {
	t.Parallel()
	now := time.Now()
	day := 24 * time.Hour
	task := &worktreeTask{Title: "Fix billing", State: "waiting"}
	running := repoLook{word: statusWaiting, icon: statusIcons[statusWaiting], tone: statusTones[statusWaiting]}
	tests := []struct {
		name   string
		row    worktreeRow
		look   repoLook
		detail string
		icon   string
		tone   string
	}{
		{"live", worktreeRow{Live: true, Task: task, Group: groupLive}, running, "Fix billing · waiting", statusIcons[statusWaiting], "warning"},
		{"now", worktreeRow{Group: groupNow}, repoLook{}, "", branchIcon, ""},
		{"recent main", worktreeRow{gitWorktree: gitWorktree{Main: true, LastOpened: now.Add(-day)}, Group: groupRecent}, repoLook{}, wordYesterday, branchIcon, ""},
		{"recent yours", worktreeRow{gitWorktree: gitWorktree{LastOpened: now.Add(-2 * day), Committed: now}, Group: groupRecent}, repoLook{}, "you · 2 d", branchIcon, ""},
		{"task", worktreeRow{gitWorktree: gitWorktree{Committed: now.Add(-20 * day)}, Task: &worktreeTask{Title: "Docs", State: wordOnBranch}, Group: groupTask}, repoLook{}, "Docs · on branch · 2 w", statusIcons[statusDone], ""},
		{"stale", worktreeRow{gitWorktree: gitWorktree{Committed: now.Add(-120 * day)}, Group: groupStale}, repoLook{}, "stale · 4 mo", statusIcons[statusDone], ""},
		{"merged", worktreeRow{gitWorktree: gitWorktree{Merged: true, Committed: now.Add(-61 * day)}, Group: groupStale}, repoLook{}, "merged · 2 mo", statusIcons[statusDone], ""},
		{"by hand", worktreeRow{gitWorktree: gitWorktree{Committed: now}, Group: groupHand}, repoLook{}, "you · today", branchIcon, ""},
		{"failed, nothing running", worktreeRow{gitWorktree: gitWorktree{Committed: now}, Task: &worktreeTask{Title: "Pause", State: wordFailed}, Group: groupTask}, repoLook{word: wordFailed, icon: statusIcons[statusError], tone: statusTones[statusError]}, "Pause · failed · today", statusIcons[statusError], "danger"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := tt.row
			row.describe(tt.look, now)
			assert.Equal(t, tt.detail, row.Detail)
			assert.Equal(t, tt.icon, row.Icon)
			assert.Equal(t, tt.tone, row.Tone)
		})
	}
}

func TestFolderWords(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	today := time.Date(2026, 11, 7, 2, 14, 0, 0, time.Local)
	before := time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		folder  worktreeFolder
		first   bool
		caption string
		byline  string
	}{
		{"first", worktreeFolder{AddedAt: before}, true, "since Oct 2", ""},
		{"added by the user", worktreeFolder{AddedAt: before}, false, "added by you · Oct 2", ""},
		{"added by a session", worktreeFolder{AddedAt: before, AddedBy: "Pause"}, false, "added by Pause · Oct 2", ""},
		{"afk", worktreeFolder{AddedAt: today, AddedBy: "Pause", AddedAfk: true}, false, "AFK · today 02:14", "added by Pause"},
		{"no date", worktreeFolder{}, true, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caption, byline := folderWords(tt.folder, tt.first, now)
			assert.Equal(t, tt.caption, caption)
			assert.Equal(t, tt.byline, byline)
		})
	}
}
