package main_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (f *fixture) additionalDirs(t *testing.T, worktree string) []string {
	t.Helper()
	created := f.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(string(c.Params), `"cwd":"`+worktree+`"`) })
	var params struct {
		AdditionalDirectories []string `json:"additionalDirectories"`
	}
	require.NoError(t, json.Unmarshal(created.Params, &params))
	return params.AdditionalDirectories
}

func TestRepoChildRunsInItsSlugWorktreeWithNoOtherFolders(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"additionalDirectories": true})
	workspace, first, second := f.twoFolders(t)
	git(t, second, "checkout", "-q", "-b", "unrelated")
	write(t, filepath.Join(second, "b.go"), "unrelated\n")
	git(t, second, "add", ".")
	git(t, second, "commit", "-qm", "unrelated")
	parent := f.doneSession(t, workspace)

	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "prompt": "hi"}, &kid))

	assert.Equal(t, filepath.Join(second, ".worktrees", "pause"), kid.Worktree)
	got := f.waitBranchChildDone(t, workspace, kid.Session)
	assert.Equal(t, parent, got.ParentSession)
	assert.Equal(t, filepath.Base(second), got.Repo)
	assert.Equal(t, second, got.RepoPath)
	assert.Equal(t, "feat/pause", git(t, kid.Worktree, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.Equal(t, git(t, second, "rev-parse", "main"), git(t, kid.Worktree, "rev-parse", "HEAD"))
	assert.Empty(t, f.additionalDirs(t, kid.Worktree))
	assert.Equal(t, []string{second}, f.additionalDirs(t, first))
}

func TestRepoChildTakesAGivenBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	parent := f.doneSession(t, workspace)

	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "branch": "team/pause", "prompt": "hi"}, &kid))

	assert.Equal(t, filepath.Join(second, ".worktrees", "pause"), kid.Worktree)
	f.waitBranchChildDone(t, workspace, kid.Session)
	assert.Equal(t, "team/pause", git(t, kid.Worktree, "rev-parse", "--abbrev-ref", "HEAD"))
}

func TestRepoChildOutsideTheWorkspaceIsRefused(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, outside := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	parent := f.doneSession(t, workspace)

	err := f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": outside, "slug": "pause"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect it first")
}

func TestRepoChildRefusesArgumentsThatDoNotFit(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	parent := f.doneSession(t, workspace)
	tests := []struct {
		name string
		args map[string]any
	}{
		{"no name for the worktree", map[string]any{"repo": repo}},
		{"unknown base", map[string]any{"repo": repo, "slug": "pause", "base": "develop"}},
		{"bad slug", map[string]any{"repo": repo, "slug": "a..b"}},
		{"slug without repo", map[string]any{"slug": "pause"}},
		{"base without repo", map[string]any{"base": "default"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, f.runFrom(t, workspace, parent, "agents.new", tt.args, nil))
		})
	}
}

func TestRepoChildTellsWhenTheFetchFailed(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	git(t, second, "remote", "add", "origin", filepath.Join(t.TempDir(), "gone"))
	parent := f.doneSession(t, workspace)

	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "prompt": "hi"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)

	notices := ofKind(f.transcript(t, workspace, kid.Session).Entries, "notice")
	require.Len(t, notices, 1)
	assert.Contains(t, string(notices[0].Notice), "main")
}

func TestPlainChildStaysAsItWasInATwoFolderWorkspace(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"additionalDirectories": true})
	workspace, first, second := f.twoFolders(t)
	parent := f.doneSession(t, workspace)

	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "hi"})

	assert.Equal(t, filepath.Join(first, ".worktrees", "c1"), kid.Worktree)
	got := f.waitBranchChildDone(t, workspace, kid.Session)
	assert.Empty(t, got.Repo)
	assert.Empty(t, got.RepoPath)
	assert.Equal(t, []string{second}, f.additionalDirs(t, kid.Worktree))
}

func TestTaskRecordsARepoDefaultBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	git(t, second, "branch", "-m", "main", "master")
	parent := f.doneSession(t, workspace)
	var task struct {
		Repos []struct {
			Into string `json:"into"`
		} `json:"repos"`
	}

	require.NoError(t, f.runAs(t, workspace, parent, "agents.task", map[string]any{"slug": "pause", "repos": []map[string]any{{"path": second, "order": 1, "landsBy": "merge", "source": "its CLAUDE.md"}}}, &task))

	require.Len(t, task.Repos, 1)
	assert.Equal(t, "master", task.Repos[0].Into)
}

func TestAgentsDriveATaskAndNeverAnswerForTheUser(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	parent := f.doneSession(t, workspace)
	third := newRepo(t)
	var asked struct {
		Request string `json:"request"`
	}

	require.NoError(t, f.runAs(t, workspace, parent, "agents.connect-repo", map[string]any{"repos": []map[string]any{{"path": third, "reason": "scope", "landsBy": "pr", "source": "its CLAUDE.md"}}}, &asked))
	require.NoError(t, f.runAs(t, workspace, parent, "agents.task", map[string]any{"slug": "pause", "repos": []map[string]any{{"path": second, "order": 1, "landsBy": "branch", "source": "no rules"}}}, nil))
	var kid struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.runAs(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "prompt": "hi"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	require.NoError(t, f.runAs(t, workspace, kid.Session, "agents.task-report", map[string]any{"state": "ready"}, nil))

	for id, args := range map[string]map[string]any{
		"agents.connect-answer": {"request": asked.Request, "answers": []map[string]any{{"path": third, "action": "connect"}}},
		"agents.land-answer":    {"request": "lr_1", "action": "merge"},
		"agents.repo-leave":     {"session": parent, "repo": second},
		"agents.repo-fix":       {"session": parent, "repo": second},
		"agents.folder-remove":  {"path": second},
	} {
		assert.ErrorContains(t, f.runAs(t, workspace, parent, id, args, nil), "unknown command", id)
	}
}
