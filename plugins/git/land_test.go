package main_test

import (
	"encoding/json"
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
	Into    string `json:"into"`
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

func baseWorktrees(t *testing.T) (string, string, string) {
	t.Helper()
	repo := committedRepo(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	a, b := filepath.Join(dir, "A"), filepath.Join(dir, "B")
	git(t, repo, "worktree", "add", "-q", "-b", "A", a)
	git(t, repo, "worktree", "add", "-q", "-b", "B", b, "A")
	git(t, repo, "config", "branch.B.clubmate-base", "A")
	write(t, filepath.Join(b, "b.go"), "two\n")
	git(t, b, "add", ".")
	git(t, b, "commit", "-qm", "two")
	return repo, a, b
}

func recordBase(t *testing.T, h *testutil.PluginHarness, repo, branch, base string) {
	t.Helper()
	value, err := json.Marshal(base)
	require.NoError(t, err)
	require.NoError(t, h.KV.Set(t.Context(), "git", baseKey(t, repo, branch), value))
}

func storedBase(t *testing.T, h *testutil.PluginHarness, repo, branch string) string {
	t.Helper()
	raw, found, err := h.KV.Get(t.Context(), "git", baseKey(t, repo, branch))
	require.NoError(t, err)
	var base string
	if found {
		require.NoError(t, json.Unmarshal(raw, &base))
	}
	return base
}

func baseKey(t *testing.T, repo, branch string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	return "worktree-base/" + real + "/" + branch
}

func TestLandDefaultsIntoBase(t *testing.T) {
	repo, _, b := baseWorktrees(t)
	main := git(t, repo, "rev-parse", "main")
	h, id := open(t, repo)
	recordBase(t, h, repo, "B", "A")
	git(t, b, "config", "branch.B.clubmate-base", "main")

	var res landed
	require.NoError(t, runCommand(t, h, id, "git.land", map[string]string{"path": b, "message": "feat: b"}, &res))

	assert.Equal(t, "A", res.Into)
	assert.Equal(t, res.Commit, git(t, repo, "rev-parse", "A"))
	assert.Equal(t, main, git(t, repo, "rev-parse", "main"))
	assert.Empty(t, storedBase(t, h, repo, "B"))
}

func TestLandRefusesIntoInAnotherCase(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo)

	err := runCommand(t, h, id, "git.land", map[string]string{"path": feat, "into": "MAIN", "message": "feat: b"}, nil)

	require.ErrorContains(t, err, "MAIN is main")
}

func TestLandKeepsTheWorktreesBaseAfterARename(t *testing.T) {
	repo := committedRepo(t)
	other := filepath.Join(repo, ".worktrees", "other")
	git(t, repo, "worktree", "add", "-q", "-b", "other", other)
	h, id := open(t, repo)
	child := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "child", "from": other, "switch": false})
	require.Empty(t, child.Error)
	write(t, filepath.Join(child.Path, "c.go"), "child\n")
	git(t, child.Path, "add", ".")
	git(t, child.Path, "commit", "-qm", "child")
	git(t, child.Path, "branch", "-m", "renamed")

	var res landed
	require.NoError(t, runCommand(t, h, id, "git.land", map[string]string{"path": child.Path, "message": "feat: c"}, &res))

	assert.Equal(t, "other", res.Into)
}

func TestWorktreeBaseIsTheAgentsPluginsAlone(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)

	err := runCommand(t, h, id, "git.worktree-base", map[string]any{"path": repo}, nil)

	require.ErrorContains(t, err, "agents plugin only")
}

func TestBranchDeleteDropsTheBase(t *testing.T) {
	repo := committedRepo(t)
	git(t, repo, "branch", "gone")
	h, id := open(t, repo)
	recordBase(t, h, repo, "gone", "main")

	require.NoError(t, h.PluginCallResult(t, "agents", "command.run", map[string]any{"id": "git.branch-delete", "workspaceId": id, "args": map[string]any{"repo": repo, "branch": "gone", "tip": git(t, repo, "rev-parse", "gone")}}, nil))

	assert.Empty(t, storedBase(t, h, repo, "gone"))
}

func TestLandWithIdleBaseAsksForInto(t *testing.T) {
	for name, idle := range map[string]func(t *testing.T, repo, a string){
		"removed":         func(t *testing.T, repo, a string) { git(t, repo, "worktree", "remove", a) },
		"deleted by hand": func(t *testing.T, _, a string) { require.NoError(t, os.RemoveAll(a)) },
	} {
		t.Run(name, func(t *testing.T) {
			repo, a, b := baseWorktrees(t)
			idle(t, repo, a)
			main := git(t, repo, "rev-parse", "main")
			h, id := open(t, repo)
			recordBase(t, h, repo, "B", "A")

			err := runCommand(t, h, id, "git.land", map[string]string{"path": b, "message": "feat: b"}, nil)

			require.ErrorContains(t, err, "base A is checked out in no worktree")
			assert.ErrorContains(t, err, "pass into")
			assert.Equal(t, main, git(t, repo, "rev-parse", "main"))
		})
	}
}

func TestLandFallsBackWhenBaseDeleted(t *testing.T) {
	repo, a, b := baseWorktrees(t)
	git(t, repo, "worktree", "remove", a)
	git(t, repo, "branch", "-D", "A")
	h, id := open(t, repo)
	recordBase(t, h, repo, "B", "A")

	var res landed
	require.NoError(t, runCommand(t, h, id, "git.land", map[string]string{"path": b, "message": "feat: b"}, &res))

	assert.Equal(t, "main", res.Into)
	assert.Equal(t, res.Commit, git(t, repo, "rev-parse", "main"))
	assert.Contains(t, res.Note, "Base A is gone, landed into main.")
}

func TestConfigBaseImportedOnce(t *testing.T) {
	for name, want := range map[string]string{"first start": "A", "imported before": "main"} {
		t.Run(name, func(t *testing.T) {
			repo, _, b := baseWorktrees(t)
			t.Setenv("SHELL", "/bin/sh")
			h := testutil.NewHarness(t)
			if want == "main" {
				require.NoError(t, h.KV.Set(t.Context(), "git", "worktree-base/imported", json.RawMessage("true")))
			}
			p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
			require.NoError(t, err)
			h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
			require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
			waitReady(t, h)

			var res landed
			require.NoError(t, runCommand(t, h, p.ID, "git.land", map[string]string{"path": b, "message": "feat: b"}, &res))

			assert.Equal(t, want, res.Into)
		})
	}
}

func TestBaseImportRetriesARepoItCouldNotRead(t *testing.T) {
	repo, _, b := baseWorktrees(t)
	broken := committedRepo(t)
	config := filepath.Join(broken, ".git", "config")
	good, err := os.ReadFile(config)
	require.NoError(t, err)
	write(t, config, string(good)+"[[[\n")
	t.Setenv("SHELL", "/bin/sh")
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	other, err := h.Svc.CreateWorkspace(t.Context(), "broken", []string{broken})
	require.NoError(t, err)
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	waitReady(t, h)
	base := func() error {
		return h.PluginCallResult(t, "agents", "command.run", map[string]any{"id": "git.worktree-base", "workspaceId": other.ID, "args": map[string]any{"path": broken}}, nil)
	}
	imported := func() bool {
		_, found, err := h.KV.Get(t.Context(), "git", "worktree-base/imported")
		require.NoError(t, err)
		return found
	}

	var res landed
	require.NoError(t, runCommand(t, h, p.ID, "git.land", map[string]string{"path": b, "message": "feat: b"}, &res))
	assert.Equal(t, "A", res.Into)
	require.Error(t, base())
	assert.False(t, imported())

	write(t, config, string(good))
	require.NoError(t, base())
	assert.True(t, imported())
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
