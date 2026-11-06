package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
)

type folderSession struct {
	Session  string `json:"session"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
}

type folderRemoveResult struct {
	Removed  bool            `json:"removed"`
	Blockers []folderSession `json:"blockers"`
	Tasks    []string        `json:"tasks"`
}

type folderImpact struct {
	Sessions  []folderSession `json:"sessions"`
	Worktrees []struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	} `json:"worktrees"`
}

func (f *fixture) twoFolders(t *testing.T) (string, string, string) {
	t.Helper()
	first, second := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, second, contract.FolderMeta{})
	require.NoError(t, err)
	return workspace, first, second
}

func (f *fixture) folderRemove(t *testing.T, workspace, path string) folderRemoveResult {
	t.Helper()
	var out folderRemoveResult
	require.NoError(t, f.run(t, workspace, "agents.folder-remove", map[string]any{"path": path}, &out))
	return out
}

func TestFolderRemoveRefusedForRunningSession(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"sleep": 2000}}})
	workspace, first, _ := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	out := f.folderRemove(t, workspace, first)

	assert.False(t, out.Removed)
	require.Len(t, out.Blockers, 1)
	assert.Equal(t, session, out.Blockers[0].Session)
	assert.Equal(t, "running", out.Blockers[0].Status)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderRemoveRefusedForWaitingSession(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"after": []map[string]any{{"permission": permissionScript()}}})
	workspace, first, _ := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitStatus(t, workspace, session, "waiting")

	out := f.folderRemove(t, workspace, first)

	assert.False(t, out.Removed)
	require.Len(t, out.Blockers, 1)
	assert.Equal(t, "waiting", out.Blockers[0].Status)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderRemoveDropsTheFolderWhenNothingRunsThere(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"sleep": 2000}}})
	workspace, first, second := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	out := f.folderRemove(t, workspace, second)

	assert.True(t, out.Removed)
	assert.Empty(t, out.Blockers)
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
}

func TestFolderRemoveRefusesAPathOutsideTheWorkspace(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, _ := f.twoFolders(t)

	err := f.run(t, workspace, "agents.folder-remove", map[string]any{"path": t.TempDir()}, nil)

	require.Error(t, err)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderImpactListsSessionsAndWorktrees(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, first, second := f.twoFolders(t)
	parent := f.doneSession(t, workspace)
	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "prompt": "hi"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)

	var there, here folderImpact
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": second}, &there))
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": first}, &here))

	require.Len(t, there.Sessions, 1)
	assert.Equal(t, kid.Session, there.Sessions[0].Session)
	require.Len(t, there.Worktrees, 1)
	assert.Equal(t, kid.Worktree, there.Worktrees[0].Path)
	assert.Equal(t, "feat/pause", there.Worktrees[0].Branch)
	require.Len(t, here.Sessions, 1)
	assert.Equal(t, parent, here.Sessions[0].Session)
	assert.Empty(t, here.Worktrees)
}

func TestFolderRemoveRefusedWhileATaskIsUnsettledThere(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)

	out := f.folderRemove(t, l.workspace, l.repos[1])

	assert.False(t, out.Removed)
	assert.Equal(t, []string{"pause"}, out.Tasks)
	assert.Len(t, f.folders(t, l.workspace), 2)
}

func TestTaskInAnotherWorkspaceDoesNotHoldTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	other := f.workspace(t, newRepo(t))
	_, err := f.h.Svc.AddRepo(t.Context(), other, l.repos[0], contract.FolderMeta{})
	require.NoError(t, err)

	out := f.folderRemove(t, other, l.repos[0])

	assert.True(t, out.Removed)
	assert.Empty(t, out.Tasks)
}
