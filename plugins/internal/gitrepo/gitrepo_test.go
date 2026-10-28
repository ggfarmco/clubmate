package gitrepo_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func TestStatusEntries(t *testing.T) {
	repo := newRepo(t)
	for _, f := range []string{"a.go", "b.go", "d.go"} {
		write(t, filepath.Join(repo, f), "package x\n")
	}
	write(t, filepath.Join(repo, ".gitignore"), "bin/\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")
	write(t, filepath.Join(repo, "a.go"), "package y\n")
	git(t, repo, "add", "a.go")
	write(t, filepath.Join(repo, "b.go"), "package y\n")
	write(t, filepath.Join(repo, "c.go"), "package x\n")
	git(t, repo, "mv", "d.go", "e.go")
	write(t, filepath.Join(repo, "bin", "x"), "x")

	top, entries, err := gitrepo.Status(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, repo, top)
	byPath := map[string]gitrepo.Entry{}
	for _, e := range entries {
		assert.True(t, filepath.IsAbs(e.Path))
		byPath[e.Path] = e
	}
	at := func(name string) gitrepo.Entry { return byPath[filepath.Join(repo, name)] }
	assert.Equal(t, "M ", string([]byte{at("a.go").X, at("a.go").Y}))
	assert.Equal(t, " M", string([]byte{at("b.go").X, at("b.go").Y}))
	assert.Equal(t, "??", string([]byte{at("c.go").X, at("c.go").Y}))
	assert.Equal(t, "R ", string([]byte{at("e.go").X, at("e.go").Y}))
	assert.Equal(t, filepath.Join(repo, "d.go"), at("e.go").Orig)
	assert.NotContains(t, byPath, filepath.Join(repo, "bin"))
}

func TestDirsWorktree(t *testing.T) {
	main := newRepo(t)
	write(t, filepath.Join(main, "a"), "a")
	git(t, main, "add", ".")
	git(t, main, "commit", "-q", "-m", "init")
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	wt := filepath.Join(base, "wt")
	git(t, main, "worktree", "add", "-q", "-b", "feat", wt)

	gitDir, commonDir, err := gitrepo.Dirs(t.Context(), wt)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(main, ".git"), commonDir)
	assert.Equal(t, filepath.Join(main, ".git", "worktrees"), filepath.Dir(gitDir))
}

func TestBranch(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, "a"), "a")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")

	b, err := gitrepo.Branch(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, "main", b)

	git(t, repo, "checkout", "-q", "--detach")
	b, err = gitrepo.Branch(t.Context(), repo)
	require.NoError(t, err)
	assert.Regexp(t, `^\(detached [0-9a-f]+\)$`, b)
}

func TestIgnoredPaths(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, ".gitignore"), "bin/\n*.log\n")
	write(t, filepath.Join(repo, "bin", "x"), "x")
	write(t, filepath.Join(repo, "a.log"), "x")
	write(t, filepath.Join(repo, "a.go"), "x")
	paths := []string{"bin", "bin/x", "a.log", "a.go"}
	for i, p := range paths {
		paths[i] = filepath.Join(repo, p)
	}

	got, err := gitrepo.Ignored(t.Context(), repo, paths)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{paths[0]: true, paths[1]: true, paths[2]: true}, got)

	got, err = gitrepo.Ignored(t.Context(), repo, paths[3:])
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMutateReturnsDespiteOrphanHoldingOutput(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, ".git", "hooks", "post-commit"), "#!/bin/sh\nsleep 8 &\n")
	require.NoError(t, os.Chmod(filepath.Join(repo, ".git", "hooks", "post-commit"), 0o700))
	write(t, filepath.Join(repo, "a"), "a")
	git(t, repo, "add", "a")

	start := time.Now()
	_, err := gitrepo.Mutate(t.Context(), repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "m")
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.ErrorIs(t, err, exec.ErrWaitDelay)
}

func TestRunLimited(t *testing.T) {
	repo := newRepo(t)
	big := strings.Repeat("x", 1<<20)
	write(t, filepath.Join(repo, "big"), big)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")

	out, truncated, err := gitrepo.RunLimited(t.Context(), repo, 10, "show", "HEAD:big")
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Equal(t, big[:10], string(out))

	out, truncated, err = gitrepo.RunLimited(t.Context(), repo, len(big), "show", "HEAD:big")
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Equal(t, big, string(out))

	_, _, err = gitrepo.RunLimited(t.Context(), repo, 10, "show", "HEAD:missing")
	ee, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok)
	assert.Contains(t, string(ee.Stderr), "missing")
}

func commitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	write(t, filepath.Join(repo, name), content)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", name)
}

func revParse(t *testing.T, repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestDefaultBase(t *testing.T) {
	master := t.TempDir()
	git(t, master, "init", "-q", "-b", "master")
	commitFile(t, master, "a", "a")
	base, err := gitrepo.DefaultBase(t.Context(), master)
	require.NoError(t, err)
	assert.Equal(t, "master", base)

	origin := t.TempDir()
	git(t, origin, "init", "-q", "-b", "develop")
	commitFile(t, origin, "a", "a")
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(clone), "clone", "-q", origin, clone)
	base, err = gitrepo.DefaultBase(t.Context(), clone)
	require.NoError(t, err)
	assert.Equal(t, "origin/develop", base)

	trunk := t.TempDir()
	git(t, trunk, "init", "-q", "-b", "trunk")
	commitFile(t, trunk, "a", "a")
	_, err = gitrepo.DefaultBase(t.Context(), trunk)
	assert.ErrorIs(t, err, gitrepo.ErrNoDefaultBase)
}

func TestMergeBaseAndChanges(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "a.go", "package x\n")
	commitFile(t, repo, "d.go", "package x\n")
	commitFile(t, repo, "HEAD", "x\n")
	baseSHA := revParse(t, repo, "HEAD")
	git(t, repo, "checkout", "-q", "-b", "feature/x")
	commitFile(t, repo, "n.go", "package x\n")
	commitFile(t, repo, "a.go", "package y\n")
	git(t, repo, "rm", "-q", "d.go")
	git(t, repo, "commit", "-q", "-m", "rm")

	mb, err := gitrepo.MergeBase(t.Context(), repo, "main", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, baseSHA, mb)

	want := map[string]byte{
		filepath.Join(repo, "n.go"): 'A',
		filepath.Join(repo, "a.go"): 'M',
		filepath.Join(repo, "d.go"): 'D',
	}
	entries, err := gitrepo.ChangedSince(t.Context(), repo, mb, "HEAD")
	require.NoError(t, err)
	got := map[string]byte{}
	for _, e := range entries {
		assert.Equal(t, byte(' '), e.Y)
		got[e.Path] = e.X
	}
	assert.Equal(t, want, got)

	git(t, repo, "checkout", "-q", "main")
	entries, err = gitrepo.ChangedSince(t.Context(), repo, mb, "feature/x")
	require.NoError(t, err)
	got = map[string]byte{}
	for _, e := range entries {
		got[e.Path] = e.X
	}
	assert.Equal(t, want, got)

	sha, err := gitrepo.Commit(t.Context(), repo, "feature/x")
	require.NoError(t, err)
	assert.Equal(t, revParse(t, repo, "feature/x"), sha)
	_, err = gitrepo.Commit(t.Context(), repo, "nope")
	assert.ErrorIs(t, err, gitrepo.ErrUnknownRef)

	_, err = gitrepo.MergeBase(t.Context(), repo, "nope", "HEAD")
	assert.ErrorIs(t, err, gitrepo.ErrUnknownRef)

	git(t, repo, "checkout", "-q", "--orphan", "lone")
	git(t, repo, "rm", "-rf", "-q", ".")
	commitFile(t, repo, "z", "z")
	_, err = gitrepo.MergeBase(t.Context(), repo, "main", "HEAD")
	assert.ErrorIs(t, err, gitrepo.ErrNoMergeBase)
}

func TestMergeBaseUnbornHead(t *testing.T) {
	repo := newRepo(t)
	_, err := gitrepo.MergeBase(t.Context(), repo, "main", "HEAD")
	assert.ErrorIs(t, err, gitrepo.ErrUnknownRef)
}

func TestBranches(t *testing.T) {
	origin := t.TempDir()
	git(t, origin, "init", "-q", "-b", "main")
	commitFile(t, origin, "a", "a")
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(clone), "clone", "-q", origin, clone)

	branches, err := gitrepo.Branches(t.Context(), clone)
	require.NoError(t, err)
	assert.Contains(t, branches, "main")
	assert.Contains(t, branches, "origin/main")
	assert.NotContains(t, branches, "origin/HEAD")
}
