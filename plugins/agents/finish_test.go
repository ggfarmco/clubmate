package main_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseTree(t *testing.T) (string, string) {
	t.Helper()
	repo := newRepo(t)
	b := filepath.Join(repo, ".worktrees", "B")
	git(t, repo, "worktree", "add", "-q", "-b", "A", filepath.Join(repo, ".worktrees", "A"))
	git(t, repo, "worktree", "add", "-q", "-b", "B", b, "A")
	git(t, repo, "config", "branch.B.clubmate-base", "A")
	return repo, b
}

func (f *fixture) recordBase(t *testing.T, repo, branch string, base any) {
	t.Helper()
	real, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	value, err := json.Marshal(base)
	require.NoError(t, err)
	require.NoError(t, f.h.KV.Set(t.Context(), "git", "worktree-base/"+real+"/"+branch, value))
}

func TestMergeFinishNamesBase(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	moved := t.TempDir()
	b := filepath.Join(moved, "B")
	git(t, repo, "worktree", "add", "-q", "-b", "A", filepath.Join(moved, "A"))
	git(t, repo, "worktree", "add", "-q", "-b", "B", b, "A")
	f := startGit(t, nil)
	f.recordBase(t, repo, "B", "A")
	workspace := f.workspace(t, b)
	require.NoError(t, f.run(t, workspace, "agents.finish-default", map[string]any{"value": "merge"}, nil))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	prompts := f.calls(t, "session/prompt")
	require.Len(t, prompts, 1)
	assert.Contains(t, string(prompts[0].Params), "it squashes your branch into A, then")
}
