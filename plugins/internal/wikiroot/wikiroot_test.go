package wikiroot_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "clubmate/internal/testutil"
	"clubmate/plugins/internal/wikiroot"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wikiroot-git")
	if err != nil {
		panic(err)
	}
	config := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(config, []byte("[maintenance]\n\tauto = false\n[user]\n\tname = t\n\temail = t@t\n[commit]\n\tgpgsign = false\n"), 0o600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", config)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func newRepo(t *testing.T, path, origin string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o700))
	git(t, path, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(path, "a.go"), []byte("package a\n"), 0o600))
	git(t, path, "add", ".")
	git(t, path, "commit", "-q", "-m", "init")
	if origin != "" {
		git(t, path, "remote", "add", "origin", origin)
	}
	return path
}

func wikiDir(t *testing.T) string {
	t.Helper()
	root := tempDir(t)
	t.Setenv("CLUBMATE_WIKI_DIR", root)
	return root
}

func TestEnsureSameRepoAllWorktrees(t *testing.T) {
	wikiDir(t)
	repo := newRepo(t, filepath.Join(tempDir(t), "clubmate"), "")
	wt := filepath.Join(tempDir(t), "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", wt)

	a, err := wikiroot.Ensure(t.Context(), repo)
	require.NoError(t, err)
	b, err := wikiroot.Ensure(t.Context(), wt)
	require.NoError(t, err)

	assert.Equal(t, a.Dir, b.Dir)
	assert.Equal(t, "clubmate", a.Name)
	assert.Equal(t, repo, b.Checkout)
	assert.DirExists(t, filepath.Join(a.Dir, ".git"))
	assert.FileExists(t, filepath.Join(a.Dir, ".wiki.json"))
}

func TestFindByOriginAfterMove(t *testing.T) {
	wikiDir(t)
	base := tempDir(t)
	repo := newRepo(t, filepath.Join(base, "a"), "https://x/a.git")
	made, err := wikiroot.Ensure(t.Context(), repo)
	require.NoError(t, err)
	moved := filepath.Join(base, "moved")
	require.NoError(t, os.Rename(repo, moved))

	found, ok, err := wikiroot.Find(t.Context(), moved)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, made.Dir, found.Dir)
	assert.Equal(t, moved, found.Checkout)
}

func TestEnsureNameCollision(t *testing.T) {
	wikiDir(t)
	first, err := wikiroot.Ensure(t.Context(), newRepo(t, filepath.Join(tempDir(t), "app"), "git@github.com:a/app.git"))
	require.NoError(t, err)
	second, err := wikiroot.Ensure(t.Context(), newRepo(t, filepath.Join(tempDir(t), "app"), "https://github.com/b/app"))
	require.NoError(t, err)

	assert.Equal(t, "app", filepath.Base(first.Dir))
	assert.Equal(t, "app-2", filepath.Base(second.Dir))
}

func TestFindNonGitFolder(t *testing.T) {
	wikiDir(t)
	folder := filepath.Join(tempDir(t), "notes")
	require.NoError(t, os.MkdirAll(folder, 0o700))

	p, err := wikiroot.Ensure(t.Context(), folder)
	require.NoError(t, err)
	assert.Empty(t, p.Origin)
	assert.Equal(t, folder, p.Checkout)

	found, ok, err := wikiroot.Find(t.Context(), folder)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, p.Dir, found.Dir)
}

func TestFindMissing(t *testing.T) {
	root := wikiDir(t)
	repo := newRepo(t, filepath.Join(tempDir(t), "x"), "")

	_, ok, err := wikiroot.Find(t.Context(), repo)
	require.NoError(t, err)
	assert.False(t, ok)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRootOverride(t *testing.T) {
	t.Setenv("CLUBMATE_WIKI_DIR", "/w/x")
	assert.Equal(t, "/w/x", wikiroot.Root())

	home := tempDir(t)
	t.Setenv("CLUBMATE_WIKI_DIR", "")
	t.Setenv("HOME", home)
	assert.Equal(t, filepath.Join(home, ".clubmate", "wiki"), wikiroot.Root())
}
