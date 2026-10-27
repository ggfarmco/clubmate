package gitrepo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

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

	top, entries, err := gitrepo.Status(t.Context(), repo, true)
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
	assert.Equal(t, byte('!'), at("bin").X)

	_, entries, err = gitrepo.Status(t.Context(), repo, false)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, filepath.Join(repo, "bin"), e.Path)
	}
}

func TestDirsWorktree(t *testing.T) {
	main := newRepo(t)
	write(t, filepath.Join(main, "a"), "a")
	git(t, main, "add", ".")
	git(t, main, "commit", "-q", "-m", "init")
	wt := filepath.Join(newRepo(t), "wt")
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
