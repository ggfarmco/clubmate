package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanupRowGroups(t *testing.T) {
	t.Parallel()
	read := func(wt folderWorktree) folderWorktree {
		wt.Read = true
		return wt
	}
	tests := []struct {
		name string
		row  cleanupRow
		want string
	}{
		{"index says merged", cleanupRow{Merged: true}, cleanupMerged},
		{"remote gone with a squash proof", cleanupRow{Merged: true, Gone: true}, cleanupPRMerged},
		{"remote gone with the child's report", cleanupRow{Report: true, Gone: true, Pushed: true}, cleanupPRMerged},
		{"unpushed commits beat the child's report", cleanupRow{Report: true, Pushed: true, State: read(folderWorktree{Unpushed: 2})}, cleanupRisky},
		{"a tip nobody pushed beats the child's report", cleanupRow{Report: true}, cleanupRisky},
		{"a squash git proves beats unpushed", cleanupRow{Gone: true, State: read(folderWorktree{Landed: true, Unpushed: 2})}, cleanupPRMerged},
		{"locked", cleanupRow{Merged: true, Locked: true}, cleanupRisky},
		{"remote gone, no proof", cleanupRow{Gone: true}, cleanupGone},
		{"read beats a cached merged", cleanupRow{Merged: true, Pushed: true, State: read(folderWorktree{})}, cleanupIdle},
		{"read proves a squash the index missed", cleanupRow{State: read(folderWorktree{Landed: true})}, cleanupMerged},
		{"idle and pushed", cleanupRow{Pushed: true}, cleanupIdle},
		{"idle with unpushed commits", cleanupRow{}, cleanupRisky},
		{"read finds uncommitted files", cleanupRow{Merged: true, State: read(folderWorktree{Landed: true, Dirty: 1})}, cleanupRisky},
		{"read finds detached commits", cleanupRow{Merged: true, State: read(folderWorktree{Landed: true, Orphans: 1})}, cleanupRisky},
		{"a repo inside", cleanupRow{Merged: true, State: read(folderWorktree{Landed: true, Nested: 1})}, cleanupRisky},
		{"unread stays out of the ticked groups", cleanupRow{Merged: true, State: read(folderWorktree{Unread: true})}, cleanupRisky},
		{"read finds unpushed commits", cleanupRow{Pushed: true, State: read(folderWorktree{Unpushed: 2})}, cleanupRisky},
		{"ignored files stay in their group", cleanupRow{State: read(folderWorktree{Landed: true, Ignored: 2})}, cleanupMerged},
		{"by hand beats everything", cleanupRow{ByHand: true, Merged: true, State: read(folderWorktree{Landed: true, Dirty: 1})}, cleanupHand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.row.group())
		})
	}
}

func TestCleanupPreTicksOnlyWhatCostsNothing(t *testing.T) {
	t.Parallel()
	free := folderWorktree{Read: true, Landed: true}
	tests := []struct {
		name string
		row  cleanupRow
		want bool
	}{
		{"merged and free", cleanupRow{Group: cleanupMerged, State: free}, true},
		{"proved PR", cleanupRow{Group: cleanupPRMerged, State: free}, true},
		{"not read yet", cleanupRow{Group: cleanupMerged}, false},
		{"ignored files", cleanupRow{Group: cleanupMerged, State: folderWorktree{Read: true, Landed: true, Ignored: 1}}, false},
		{"an open session", cleanupRow{Group: cleanupMerged, Closes: []string{"agt_1"}, State: free}, false},
		{"no proof", cleanupRow{Group: cleanupGone, State: free}, false},
		{"idle", cleanupRow{Group: cleanupIdle, State: free}, false},
		{"by hand", cleanupRow{Group: cleanupHand, State: free}, false},
		{"risky", cleanupRow{Group: cleanupRisky, State: free}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.row.preTick())
		})
	}
}

func TestCleanupGroupsReconcile(t *testing.T) {
	t.Parallel()
	plan := cleanupPlan{Rows: []cleanupRow{
		{Path: "/a", Merged: true},
		{Path: "/b", Merged: true, Gone: true},
		{Path: "/c", Gone: true},
		{Path: "/d", Pushed: true},
		{Path: "/e"},
		{Path: "/f", ByHand: true},
	}}

	plan.regroup()

	assert.Equal(t, "6 · 5 stale, 1 by hand", plan.Summary)
	total := 0
	for _, g := range plan.Groups {
		total += g.Count
	}
	assert.Equal(t, plan.Total, total)
	assert.Equal(t, plan.Total, plan.Stale+plan.ByHand)
}

func TestPickerOffersCleanUpOnlyWithSomethingToClean(t *testing.T) {
	t.Parallel()
	folders := []worktreeFolder{{Path: "/r", Name: "r"}}
	rows := []worktreeRow{{gitWorktree: gitWorktree{Repo: "/r"}, Stale: true, Group: groupStale}}

	assert.Equal(t, "Clean up worktrees…", buildPicker("ads", folders, rows).Cleanup)
	assert.Empty(t, buildPicker("ads", folders, []worktreeRow{{gitWorktree: gitWorktree{Repo: "/r"}, Group: groupOther}}).Cleanup)
	assert.Equal(t, "Clean up 1…", folderSections("/r", rows)[0].Clean)
}
