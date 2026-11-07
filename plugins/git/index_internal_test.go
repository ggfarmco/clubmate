package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func TestIndexBatchStaysFlatAt400Worktrees(t *testing.T) {
	few := batchProcesses(t, 1)
	many := batchProcesses(t, 400)

	assert.Equal(t, few, many)
}

func batchProcesses(t *testing.T, worktrees int) int {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	for i := range worktrees {
		name := fmt.Sprintf("w%d", i)
		gitIn(t, repo, "worktree", "add", "-q", "--no-checkout", "-b", name, filepath.Join(repo, ".worktrees", name))
	}
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)

	f, err := gitrepo.OpenFolder(t.Context(), repo)
	require.NoError(t, err)
	idx, err := readFolderIndex(t.Context(), &folder{Folder: f, root: repo}, nil)
	require.NoError(t, err)
	require.Len(t, idx.entries, worktrees+1)

	raw, err := os.ReadFile(trace)
	require.NoError(t, err)
	count := strings.Count(string(raw), "trace: built-in: git ")
	require.Positive(t, count)
	return count
}

func gitIn(t *testing.T, repo string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}
