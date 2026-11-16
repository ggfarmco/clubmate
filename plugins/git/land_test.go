package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

type landed struct {
	Commit  string `json:"commit"`
	Removed bool   `json:"removed"`
	Note    string `json:"note"`
}

func featWorktree(t *testing.T) (string, string) {
	t.Helper()
	repo := committedRepo(t)
	write(t, filepath.Join(repo, "s.go"), "one\n")
	git(t, repo, "add", "s.go")
	git(t, repo, "commit", "-qm", "s")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	feat := filepath.Join(dir, "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	write(t, filepath.Join(feat, "b.go"), "two\n")
	write(t, filepath.Join(feat, "s.go"), "two\n")
	git(t, feat, "add", ".")
	git(t, feat, "commit", "-qm", "two")
	return repo, feat
}

func preCommit(t *testing.T, repo, script string) {
	t.Helper()
	hooks := filepath.Join(repo, ".git", "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\n"+script+"\n"), 0o755))
}

func TestLandRefusesATreeTheCommitHookChanged(t *testing.T) {
	repo, feat := featWorktree(t)
	preCommit(t, repo, "echo fmt >> b.go; echo fmt >> s.go; git add b.go s.go a.go n.go")
	write(t, filepath.Join(repo, "a.go"), "mine\n")
	write(t, filepath.Join(repo, "n.go"), "new\n")
	old := git(t, repo, "rev-parse", "main")
	h, id := open(t, repo)
	land := map[string]string{"path": feat, "message": "feat: b"}

	err := runCommand(t, h, id, "git.land", land, nil)

	require.ErrorContains(t, err, "commit hook changed the committed tree (a.go, b.go, n.go, s.go)")
	assert.Equal(t, old, git(t, repo, "rev-parse", "main"))
	assert.Empty(t, git(t, repo, "diff", "--cached", "--name-only"))
	assert.Equal(t, "a.go", git(t, repo, "diff", "--name-only"))
	assert.Equal(t, "n.go", git(t, repo, "ls-files", "--others"))
	for name, want := range map[string]string{"a.go": "mine\n", "n.go": "new\n", "s.go": "one\n"} {
		got, err := os.ReadFile(filepath.Join(repo, name))
		require.NoError(t, err)
		assert.Equal(t, want, string(got), name)
	}
	assert.NoFileExists(t, filepath.Join(repo, "b.go"))
	assert.Equal(t, "two", git(t, repo, "log", "-1", "--format=%s", "feat"))
	preCommit(t, repo, "")
	require.NoError(t, runCommand(t, h, id, "git.land", land, nil))
	assert.Equal(t, "feat: b", git(t, repo, "log", "-1", "--format=%s", "main"))
}

func TestLandKeepsASourceBranchThatMovedMeanwhile(t *testing.T) {
	repo, feat := featWorktree(t)
	preCommit(t, repo, "unset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE\ngit -C '"+feat+"' commit -q --no-verify --allow-empty -m later")
	h, id := open(t, repo)

	var res landed
	require.NoError(t, runCommand(t, h, id, "git.land", map[string]string{"path": feat, "message": "feat: b"}, &res))

	assert.Equal(t, res.Commit, git(t, repo, "rev-parse", "main"))
	assert.False(t, res.Removed)
	assert.Contains(t, res.Note, "feat moved")
	assert.DirExists(t, feat)
	assert.Equal(t, "later", git(t, repo, "log", "-1", "--format=%s", "feat"))
}

func TestLandIndexRefusalKeepsTheStagedChange(t *testing.T) {
	repo, _ := featWorktree(t)
	preCommit(t, repo, "git add a.go n.go")
	write(t, filepath.Join(repo, "c.go"), "staged\n")
	git(t, repo, "add", "c.go")
	write(t, filepath.Join(repo, "a.go"), "mine\n")
	write(t, filepath.Join(repo, "n.go"), "new\n")
	old := git(t, repo, "rev-parse", "main")
	h, id := open(t, repo)

	err := runCommand(t, h, id, "git.land", map[string]string{"path": repo, "mode": "index", "message": "feat: c"}, nil)

	require.ErrorContains(t, err, "commit hook changed")
	assert.Equal(t, old, git(t, repo, "rev-parse", "main"))
	assert.Equal(t, "c.go", git(t, repo, "diff", "--cached", "--name-only"))
	for name, want := range map[string]string{"a.go": "mine\n", "c.go": "staged\n", "n.go": "new\n"} {
		got, err := os.ReadFile(filepath.Join(repo, name))
		require.NoError(t, err)
		assert.Equal(t, want, string(got), name)
	}
}

func TestLandKeepsAWorktreeOpenInTheEditor(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, stopEditorManifest))
	testutil.Eventually(t, func() bool {
		return !slices.ContainsFunc(h.Sup.Statuses(), func(s plugin.Status) bool { return s.State != "running" })
	}, readyWait)
	_, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: id, Title: "nvim", Owner: "editor-nvim", Command: []string{"sh", "-c", "exec cat"}, Cwd: feat})
	require.NoError(t, err)

	var res landed
	require.NoError(t, runCommand(t, h, id, "git.land", map[string]string{"path": feat, "message": "feat: b"}, &res))

	assert.Equal(t, res.Commit, git(t, repo, "rev-parse", "main"))
	assert.False(t, res.Removed)
	assert.Equal(t, "Kept the worktree and branch: it is open in the editor; leave it, the user removes it from the app.", res.Note)
	assert.DirExists(t, feat)
	assert.Equal(t, "two", git(t, repo, "log", "-1", "--format=%s", "feat"))
	testutil.Never(t, func() bool { return len(commandArgs(t, h, "editor-nvim.stop-root", 0)) > 0 }, 250*time.Millisecond)
}
