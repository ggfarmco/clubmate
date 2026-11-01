package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/plugins/internal/gitrepo"
)

func TestScope(t *testing.T) {
	root := realDir(t)
	repo, linked, uploads := filepath.Join(root, "repo"), filepath.Join(root, "linked"), filepath.Join(root, "uploads")
	gitRepo(t, repo, linked)
	write(t, filepath.Join(repo, "a.md"), "# A")
	write(t, filepath.Join(linked, "b.md"), "# B")
	write(t, filepath.Join(uploads, "agt_a", "x", "c.md"), "# C")
	write(t, filepath.Join(repo, ".env"), "SECRET=1")
	write(t, filepath.Join(repo, "big.md"), string(make([]byte, maxDoc+1)))
	allow := testScope(t, repo, uploads)

	tests := []struct {
		name string
		path string
		err  string
	}{
		{"folder", filepath.Join(repo, "a.md"), ""},
		{"linked worktree", filepath.Join(linked, "b.md"), ""},
		{"uploads", filepath.Join(uploads, "agt_a", "x", "c.md"), ""},
		{"outside", "/etc/x.md", "not in workspace"},
		{"not a document", filepath.Join(repo, ".env"), "not a document"},
		{"too large", filepath.Join(repo, "big.md"), "too large"},
		{"missing", filepath.Join(repo, "gone.md"), "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := load(tt.path, allow)
			if tt.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.err)
		})
	}
}

func TestScopeSymlinkOut(t *testing.T) {
	root := realDir(t)
	repo, outside := filepath.Join(root, "repo"), filepath.Join(root, "outside")
	gitRepo(t, repo, "")
	write(t, filepath.Join(outside, "config.md"), "# secret")
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "docs")))

	_, _, err := load(filepath.Join(repo, "docs", "config.md"), testScope(t, repo, ""))

	assert.EqualError(t, err, "not in workspace")
}

func testScope(t *testing.T, repo, uploads string) func(string) bool {
	t.Helper()
	trees, err := gitrepo.Worktrees(t.Context(), repo)
	require.NoError(t, err)
	ws := contract.Workspace{ID: "ws", Folders: []contract.Folder{{Path: repo}}}
	return allowed(ws, map[string][]gitrepo.Worktree{repo: trees}, uploads)
}

func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func gitRepo(t *testing.T, repo, linked string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(repo, 0o700))
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	}
	if linked != "" {
		steps = append(steps, []string{"worktree", "add", "-q", "-b", "feat", linked})
	}
	for _, args := range steps {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
