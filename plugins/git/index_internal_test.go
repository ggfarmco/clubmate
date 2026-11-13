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

func TestIndexMergeWalkStopsAtOldestMergedTip(t *testing.T) {
	repo := t.TempDir()
	at := func(date string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+date+" +0000", "GIT_AUTHOR_DATE="+date+" +0000")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	at("1700001000", "init", "-q", "-b", "main")
	at("1700001000", "commit", "-q", "--allow-empty", "-m", "init")
	for _, b := range []struct{ name, tip, merge string }{{"skewed", "1700002000", "1700001500"}, {"fresh", "1700002500", "1700003000"}} {
		at(b.tip, "checkout", "-q", "-b", b.name, "main")
		at(b.tip, "commit", "-q", "--allow-empty", "-m", b.name)
		at(b.merge, "checkout", "-q", "main")
		at(b.merge, "merge", "-q", "--no-ff", "-m", "merge "+b.name, b.name)
		gitIn(t, repo, "worktree", "add", "-q", "--no-checkout", filepath.Join(repo, ".worktrees", b.name), b.name)
	}

	f, err := gitrepo.OpenFolder(t.Context(), repo)
	require.NoError(t, err)
	idx, err := readFolderIndex(t.Context(), &folder{Folder: f, root: repo}, nil)
	require.NoError(t, err)
	merged := map[string]bool{}
	for _, e := range idx.entries {
		merged[e.Branch] = e.Merged
	}

	assert.True(t, merged["fresh"])
	assert.False(t, merged["skewed"], "a merge dated before the oldest merged tip is past the walk and reads unmerged")
}
