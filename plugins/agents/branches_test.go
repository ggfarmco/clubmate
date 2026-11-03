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
	assert.Empty(t, c.name(context.Background(), root))

	require.NoError(t, os.MkdirAll(root, 0o755))
	out, err := exec.Command("git", "-C", root, "init", "-q", "-b", "feat/x").CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, "feat/x", c.name(context.Background(), root))
}
