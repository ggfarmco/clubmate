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

func TestAttributeNotesTurnCommitsAndBlameFindsThem(t *testing.T) {
	ctx := context.Background()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("one\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "init")
	git("switch", "-qc", "side")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "b.go"), []byte("side\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "side")
	git("switch", "-q", "-")
	cp, err := checkpoint(ctx, repo, "agt_0000000000000001", 2)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("one\ntwo\nthree\n"), 0o600))
	git("commit", "-qam", "agent work")
	commit := git("rev-parse", "HEAD")
	git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	merge := git("rev-parse", "HEAD")

	turn := attribution{Session: "agt_0000000000000001", Turn: 2, Agent: "claude", Title: "Fix", Prompt: "add lines\nand more", Checkpoint: cp}
	got, err := attribute(ctx, repo, turn)
	require.NoError(t, err)
	assert.Equal(t, []string{merge, commit}, got)

	again, err := attribute(ctx, repo, attribution{Session: "agt_0000000000000002", Turn: 1, Checkpoint: cp})
	require.NoError(t, err)
	assert.Empty(t, again)

	ranges, err := blame(ctx, filepath.Join(repo, "a.go"), 0, 0)
	require.NoError(t, err)
	want := turn
	want.Commit, want.Prompt, want.StartLine, want.EndLine = commit, "add lines", 2, 3
	assert.Equal(t, []attribution{want}, ranges)

	byCommit, err := commitAttribution(ctx, repo, "HEAD^")
	require.NoError(t, err)
	want.StartLine, want.EndLine = 0, 0
	assert.Equal(t, want, byCommit)
}
