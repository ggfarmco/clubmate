package gitrepo_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestMutateInertRunsNothingWithoutItsOverrides(t *testing.T) {
	repo := newRepo(t)
	bin, ran := t.TempDir(), filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --get-regexp ] && exit 128; done\ntouch "+ran+"\n")
	require.NoError(t, os.Chmod(filepath.Join(bin, "git"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := gitrepo.MutateInert(t.Context(), repo, "status")

	assert.Error(t, err)
	assert.NoFileExists(t, ran)
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

func TestValidBranchName(t *testing.T) {
	for _, name := range []string{"main", "feature/x", "origin/team/DUE-970", "v1.2"} {
		assert.True(t, gitrepo.ValidBranchName(name), name)
	}
	for _, name := range []string{"", "-x", "a..b", "a b", "x@{1}", "a/", "/a", "a//b", ".a", "a/.b", "a.lock", "a.", "a:b", "a\\b"} {
		assert.False(t, gitrepo.ValidBranchName(name), name)
	}
}

func TestSnapshotPrunesScratchOfDeadProcesses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	parent := filepath.Join(os.TempDir(), "clubmate-cp")
	dead := exec.Command("/usr/bin/true")
	require.NoError(t, dead.Run())
	stale := filepath.Join(parent, strconv.Itoa(dead.ProcessState.Pid())+"-1")
	live := filepath.Join(parent, strconv.Itoa(os.Getpid())+"-1")
	require.NoError(t, os.MkdirAll(stale, 0o700))
	require.NoError(t, os.MkdirAll(live, 0o700))
	repo := newRepo(t)
	write(t, filepath.Join(repo, "a"), "a")

	tree, _, err := gitrepo.Snapshot(t.Context(), repo)
	require.NoError(t, err)
	assert.NotEmpty(t, tree)
	assert.NoDirExists(t, stale)
	assert.DirExists(t, live)
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestSnapshotRefusesSharedScratchParent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	require.NoError(t, os.Mkdir(filepath.Join(os.TempDir(), "clubmate-cp"), 0o777))
	require.NoError(t, os.Chmod(filepath.Join(os.TempDir(), "clubmate-cp"), 0o777))

	_, _, err := gitrepo.Snapshot(t.Context(), newRepo(t))
	assert.Error(t, err)
}

func TestDefaultBranchDropsTheRemote(t *testing.T) {
	origin := newRepo(t)
	git(t, origin, "commit", "-q", "--allow-empty", "-m", "init")
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, origin, "clone", "-q", origin, clone)

	name, err := gitrepo.DefaultBranch(t.Context(), clone)

	require.NoError(t, err)
	assert.Equal(t, "main", name)
}

func TestCloneDisablesTerminalPrompts(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "seen")
	fakeGit(t, `echo "$GIT_TERMINAL_PROMPT" > "`+seen+`"`)

	require.NoError(t, gitrepo.Clone(t.Context(), "url", "into"))

	got, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Equal(t, "0\n", string(got))
}

func TestCloneTerminatesGitOnCancel(t *testing.T) {
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "ready"), filepath.Join(dir, "stopped")
	fakeGit(t, `trap 'touch "`+stopped+`"; exit 1' TERM
touch "`+ready+`"
sleep 5 >/dev/null 2>&1 &
wait`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- gitrepo.Clone(ctx, "url", "into") }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)

	cancel()

	assert.Error(t, <-done)
	assert.FileExists(t, stopped)
}

func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "git"), "#!/bin/sh\n"+script+"\n")
	require.NoError(t, os.Chmod(filepath.Join(dir, "git"), 0o700))
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+os.Getenv("PATH"))
}
