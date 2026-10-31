package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceSplitsTurnAndHumanLines(t *testing.T) {
	ctx := context.Background()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600))
	}
	git("init", "-q")
	write("a.go", "one\n")
	git("add", ".")
	git("commit", "-qm", "init")
	start := git("rev-parse", "HEAD")
	const session = "agt_0000000000000001"
	write("b.go", "human\n")
	cp, err := checkpoint(ctx, repo, session, 1)
	require.NoError(t, err)
	write("a.go", "one\nai\n")
	git("commit", "-qam", "agent one")
	write("c.go", "ai\n")
	git("add", "b.go", "c.go")
	git("commit", "-qm", "agent two")
	_, err = attribute(ctx, repo, attribution{Session: session, Turn: 1, Checkpoint: cp})
	require.NoError(t, err)
	write("d.go", "ai late\n")
	_, err = endCheckpoint(ctx, repo, session, 1)
	require.NoError(t, err)
	write("d.go", "ai late\nhuman\n")
	git("add", "d.go")
	git("commit", "-qm", "human")

	records, err := trace(ctx, repo, start+"..HEAD")
	require.NoError(t, err)

	ai := traceConversation{URL: fmt.Sprintf("clubmate://session/%s#turn=1", session), Contributor: traceContributor{Type: contributorAI}}
	human := traceConversation{Contributor: traceContributor{Type: contributorHuman}}
	lines := func(c traceConversation, start, end int) traceConversation {
		c.Ranges = []traceRange{{StartLine: start, EndLine: end}}
		return c
	}
	want := [][]traceFile{
		{{Path: "a.go", Conversations: []traceConversation{lines(ai, 2, 2)}}},
		{{Path: "b.go", Conversations: []traceConversation{lines(human, 1, 1)}}, {Path: "c.go", Conversations: []traceConversation{lines(ai, 1, 1)}}},
		{{Path: "d.go", Conversations: []traceConversation{lines(ai, 1, 1), lines(human, 2, 2)}}},
	}
	require.Len(t, records, len(want))
	for i, rec := range records {
		assert.Equal(t, want[i], rec.Files, "commit %d", i+1)
		assert.Equal(t, git("rev-parse", fmt.Sprintf("HEAD~%d", len(want)-1-i)), rec.VCS.Revision)
	}
	again, err := trace(ctx, repo, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, records[2].ID, again[0].ID)
}
