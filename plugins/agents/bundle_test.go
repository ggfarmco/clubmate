package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleCarriesOnlyTheSessionsNotes(t *testing.T) {
	ctx := context.Background()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo, clone := filepath.Join(tmp, "repo"), filepath.Join(tmp, "clone")
	git := func(dir string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	require.NoError(t, os.Mkdir(repo, 0o700))
	git(repo, "init", "-q")
	git(repo, "commit", "-q", "--allow-empty", "-m", "init")
	git(tmp, "clone", "-q", repo, clone)
	const mine, other = "agt_0000000000000001", "agt_0000000000000002"
	cp, err := checkpoint(ctx, repo, mine, 1)
	require.NoError(t, err)
	git(repo, "commit", "-q", "--allow-empty", "-m", "mine")
	ours := git(repo, "rev-parse", "HEAD")
	_, err = attribute(ctx, repo, attribution{Session: mine, Turn: 1, Checkpoint: cp})
	require.NoError(t, err)
	git(repo, "commit", "-q", "--allow-empty", "-m", "other")
	theirs := git(repo, "rev-parse", "HEAD")
	git(repo, "notes", "--ref", notesRef, "add", "-m", noteSession+": "+other, theirs)
	git(clone, "fetch", "-q", "origin", "HEAD")

	git(clone, "notes", "--ref", notesRef, "add", "-m", noteSession+": "+other, theirs)
	bundle := filepath.Join(tmp, bundleRefs)
	require.NoError(t, bundleRefsOf(ctx, record{ID: mine, Worktree: repo}, []string{ours}, bundle))
	require.NoError(t, fetchRefs(ctx, clone, bundle, mine))

	assert.Equal(t, cp, git(clone, "rev-parse", checkpointRef(mine, 1)))
	assert.Contains(t, git(clone, "notes", "--ref", notesRef, "show", ours), noteSession+": "+mine)
	assert.Len(t, strings.Fields(git(clone, "notes", "--ref", notesRef, "list")), 4)
	assert.Empty(t, git(repo, "for-each-ref", exportNotes))
}
