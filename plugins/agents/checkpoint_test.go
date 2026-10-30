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

func TestCheckpointUnbornHead(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, exec.Command("git", "-C", repo, "init", "-q").Run())
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o600))

	sha, err := checkpoint(context.Background(), repo, "agt_0000000000000001", 1)
	require.NoError(t, err)

	out, err := exec.Command("git", "-C", repo, "show", "-s", "--format=%P", sha).Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(out)))
	out, err = exec.Command("git", "-C", repo, "show", sha+":a.txt").Output()
	require.NoError(t, err)
	assert.Equal(t, "a\n", string(out))
	assert.NoFileExists(t, filepath.Join(repo, ".git", "index"))
}
