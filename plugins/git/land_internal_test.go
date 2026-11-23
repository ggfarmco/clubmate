package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func TestSummaryGoesBeforeTheTrailersWrappedAt100(t *testing.T) {
	repo := branchRepo(t)
	long := "- " + strings.Repeat("word ", 30) + "end"
	tests := []struct {
		name, message, want string
	}{
		{"no trailers", "feat: b\n\nBody.", "feat: b\n\nBody.\n\nSums it up."},
		{"trailers", "feat: b\n\nBody.\n\nCo-Authored-By: A <a@a>\nSigned-off-by: B <b@b>\n", "feat: b\n\nBody.\n\nSums it up.\n\nCo-Authored-By: A <a@a>\nSigned-off-by: B <b@b>"},
		{"subject only", "feat: b", "feat: b\n\nSums it up."},
		{"crlf trailers", "feat: b\r\n\r\nBody.\r\n\r\nCo-Authored-By: A <a@a>\r\n", "feat: b\n\nBody.\n\nSums it up.\n\nCo-Authored-By: A <a@a>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withSummary(t.Context(), repo, tt.message, "Sums it up.")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	got, err := withSummary(t.Context(), repo, "feat: b", long)

	require.NoError(t, err)
	lines := strings.Split(got, "\n")[2:]
	require.Greater(t, len(lines), 1)
	for _, line := range lines {
		assert.LessOrEqual(t, len(line), summaryWidth, line)
	}
	assert.True(t, strings.HasPrefix(lines[1], "  word"), lines[1])
	assert.Equal(t, strings.Fields(long), strings.Fields(strings.Join(lines, " ")))
}

func TestWrapLeavesALongLineOfUnicodeSpacesEmpty(t *testing.T) {
	assert.Equal(t, "a\n", wrapText("a\n"+strings.Repeat("\u00a0\u3000", 60), summaryWidth))
}

func hookedPlan(t *testing.T, name, script string) (*landPlan, string) {
	t.Helper()
	repo := branchRepo(t)
	hook := filepath.Join(repo, ".git", "hooks", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(hook), 0o755))
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	p := &landPlan{target: gitrepo.Worktree{Path: repo}, into: "main", source: "feat", old: gitT(t, repo, "rev-parse", "main"), head: gitT(t, repo, "rev-parse", "feat")}
	tree, err := mergeTree(t.Context(), repo, p.old, p.head, p.into)
	require.NoError(t, err)
	p.tree = tree
	return p, repo
}

func TestLandRetriesWithoutTheSummaryWhenTheCommitMsgHookRefuses(t *testing.T) {
	p, repo := hookedPlan(t, "commit-msg", "if grep -q Sums \"$1\"; then echo body line too long >&2; exit 1; fi")

	commit, note, err := p.commitWithSummary(t.Context(), "feat: b", "Sums it up.")

	require.NoError(t, err)
	assert.Equal(t, commit, gitT(t, repo, "rev-parse", "main"))
	assert.Equal(t, "feat: b", gitT(t, repo, "log", "-1", "--format=%B", "main"))
	assert.Contains(t, note, "landed without the summary")
	assert.Contains(t, note, "body line too long")
}

func TestLandRetryReportsTheTreeAPreCommitHookChanged(t *testing.T) {
	p, repo := hookedPlan(t, "commit-msg", "if grep -q Sums \"$1\"; then echo body line too long >&2; exit 1; fi")
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\necho fmt >> b.go; git add b.go\n"), 0o755))

	_, _, err := p.commitWithSummary(t.Context(), "feat: b", "Sums it up.")

	require.ErrorIs(t, err, errLandHook)
	assert.NotContains(t, err.Error(), "body line too long")
	assert.Equal(t, p.old, gitT(t, repo, "rev-parse", "main"))
}

func TestLandKeepsThePreCommitHooksErrorWithASummary(t *testing.T) {
	p, repo := hookedPlan(t, "pre-commit", "echo fmt >> b.go; echo needs formatting >&2; exit 1")

	_, _, err := p.commitWithSummary(t.Context(), "feat: b", "Sums it up.")

	require.ErrorContains(t, err, "needs formatting")
	assert.Equal(t, p.old, gitT(t, repo, "rev-parse", "main"))
}

func TestLandKeepsTheHooksErrorWhenItOutlastsTheCall(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "started")
	p, repo := hookedPlan(t, "commit-msg", "touch '"+mark+"'; sleep 5 >/dev/null 2>&1; exit 1")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(mark); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	_, _, err := p.commitWithSummary(ctx, "feat: b", "Sums it up.")

	_, failed := errors.AsType[commitFailed](err)
	assert.True(t, failed, "%v", err)
	assert.NotErrorIs(t, err, errLandMoved)
	assert.Equal(t, p.old, gitT(t, repo, "rev-parse", "main"))
}

func TestRefExistsTellsAMissingRefFromAGitError(t *testing.T) {
	repo := branchRepo(t)

	exists, err := refExists(t.Context(), repo, "refs/heads/gone")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = refExists(t.Context(), filepath.Join(repo, "nowhere"), "refs/heads/main")
	assert.Error(t, err)
}
