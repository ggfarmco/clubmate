package main_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
)

type worktreesTask struct {
	Session string `json:"session"`
	Title   string `json:"title"`
	State   string `json:"state"`
}

type worktreesRow struct {
	Repo   string         `json:"repo"`
	Path   string         `json:"path"`
	Branch string         `json:"branch"`
	MadeBy string         `json:"madeBy"`
	Live   bool           `json:"live"`
	Task   *worktreesTask `json:"task"`
	ByHand bool           `json:"byHand"`
	Stale  bool           `json:"stale"`
	Group  string         `json:"group"`
}

type worktreesFolder struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	AddedBy   string    `json:"addedBy"`
	AddedAt   time.Time `json:"addedAt"`
	AddedAfk  bool      `json:"addedAfk"`
	New       bool      `json:"new"`
	Worktrees int       `json:"worktrees"`
	Live      int       `json:"live"`
}

type worktreesResult struct {
	Folders []worktreesFolder `json:"folders"`
	Tasks   []struct {
		Session   string   `json:"session"`
		Folders   []string `json:"folders"`
		Worktrees []string `json:"worktrees"`
	} `json:"tasks"`
	Worktrees []worktreesRow `json:"worktrees"`
}

func (f *fixture) worktrees(t *testing.T, workspace string) worktreesResult {
	t.Helper()
	var out worktreesResult
	require.NoError(t, f.run(t, workspace, "agents.worktrees", nil, &out))
	return out
}

func TestWorktreesJoinsATaskAcrossTwoFolders(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, second := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, second, contract.FolderMeta{AddedBy: "boss", AddedAfk: true})
	require.NoError(t, err)
	l := f.startTask(t, workspace, "merge", "pause", []string{first, second}, []int{1, 2})

	got := f.worktrees(t, workspace)

	require.Len(t, got.Tasks, 1)
	assert.Equal(t, l.owner, got.Tasks[0].Session)
	assert.Equal(t, []string{first, second}, got.Tasks[0].Folders)
	assert.ElementsMatch(t, []string{l.kids[0].Worktree, l.kids[1].Worktree}, got.Tasks[0].Worktrees)
	for _, kid := range l.kids {
		i := -1
		for j, row := range got.Worktrees {
			if row.Path == kid.Worktree {
				i = j
			}
		}
		require.GreaterOrEqual(t, i, 0)
		assert.Equal(t, "agents", got.Worktrees[i].MadeBy)
		assert.False(t, got.Worktrees[i].ByHand)
		require.NotNil(t, got.Worktrees[i].Task)
		assert.Equal(t, l.owner, got.Worktrees[i].Task.Session)
	}
	require.Len(t, got.Folders, 2)
	assert.Equal(t, filepath.Base(first), got.Folders[0].Name)
	assert.False(t, got.Folders[0].New)
	assert.Equal(t, 2, got.Folders[0].Worktrees)
	assert.Equal(t, "boss", got.Folders[1].AddedBy)
	assert.False(t, got.Folders[1].AddedAt.IsZero())
	assert.True(t, got.Folders[1].AddedAfk)
	assert.True(t, got.Folders[1].New)
}

func TestWorktreesCallsAWorktreeNobodyMadeByHand(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	dir := filepath.Join(repo, ".worktrees", "mine")
	git(t, repo, "worktree", "add", "-q", "-b", "mine", dir)

	got := f.worktrees(t, workspace)

	require.Len(t, got.Worktrees, 2)
	for _, row := range got.Worktrees {
		assert.Equal(t, row.Path == dir, row.ByHand, row.Path)
	}
}

func TestWorktreeCommandsStayOutOfAgentsReach(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.doneSession(t, workspace)

	for _, command := range []string{"agents.worktrees", "git.worktree-index", "git.worktree-state"} {
		assert.ErrorContains(t, f.runAs(t, workspace, session, command, map[string]any{}, nil), "unknown command", command)
	}
}
