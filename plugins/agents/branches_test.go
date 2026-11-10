package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchCacheRetriesWorktreeCreatedLater(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "feat", "x")
	var c branchCache
	assert.Empty(t, c.name(context.Background(), "w", root))

	require.NoError(t, os.MkdirAll(root, 0o755))
	out, err := exec.Command("git", "-C", root, "init", "-q", "-b", "feat/x").CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, "feat/x", c.name(context.Background(), "w", root))
}

func TestBranchCacheResetsOnlyTheChangedWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	one, two := filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")
	for _, root := range []string{one, two} {
		out, err := exec.Command("git", "init", "-q", "-b", "main", root).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	var c branchCache
	assert.Equal(t, "main", c.name(ctx, "w1", one))
	assert.Equal(t, "main", c.name(ctx, "w2", two))
	for _, root := range []string{one, two} {
		out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "next").CombinedOutput()
		require.NoError(t, err, string(out))
	}

	c.reset("w1")
	assert.Equal(t, "next", c.name(ctx, "w1", one))
	assert.Equal(t, "main", c.name(ctx, "w2", two))
}
