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

func carryGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func carryCommit(t *testing.T, repo, parent, file, content string) string {
	t.Helper()
	carryGit(t, repo, "checkout", "-q", "--detach", parent)
	require.NoError(t, os.WriteFile(filepath.Join(repo, file), []byte(content), 0o600))
	carryGit(t, repo, "add", ".")
	carryGit(t, repo, "commit", "-qm", file)
	return carryGit(t, repo, "rev-parse", "HEAD")
}

func carryCase(t *testing.T, moved, landed string) bool {
	t.Helper()
	repo := unitRepo(t)
	old0 := carryCommit(t, repo, "main", "b.go", "one\n")
	tree0 := carryGit(t, repo, "rev-parse", carryCommit(t, repo, old0, "a.go", "land\n")+"^{tree}")
	old1 := carryCommit(t, repo, old0, moved, "moved\n")
	tree1 := carryGit(t, repo, "rev-parse", carryCommit(t, repo, old1, "a.go", landed)+"^{tree}")
	ok, err := carryable(context.Background(), repo, old0, tree0, old1, tree1)
	require.NoError(t, err)
	return ok
}

func TestCarryableDisjointBaseMove(t *testing.T) {
	t.Parallel()
	assert.True(t, carryCase(t, "b.go", "land\n"))
}

func TestNotCarryableOverlap(t *testing.T) {
	t.Parallel()
	assert.False(t, carryCase(t, "a.go", "land\n"))
}

func TestNotCarryableChangedPatch(t *testing.T) {
	t.Parallel()
	assert.False(t, carryCase(t, "b.go", "other\n"))
}
