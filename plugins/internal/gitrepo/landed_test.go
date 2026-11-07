package gitrepo_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func landedRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "config", "user.name", "t")
	gitIn(t, repo, "config", "user.email", "t@t")
	commitAt(t, repo, "a.txt", "one\n")
	return repo
}

func gitIn(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func commitAt(t *testing.T, repo, name, content string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600))
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-qm", name)
	return gitIn(t, repo, "rev-parse", "HEAD")
}

func TestLanded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		land func(t *testing.T, repo, tip string)
		want bool
	}{
		{"not landed", func(*testing.T, string, string) {}, false},
		{"merged", func(t *testing.T, repo, _ string) {
			gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "m", "task")
		}, true},
		{"fast-forward", func(t *testing.T, repo, _ string) {
			gitIn(t, repo, "merge", "-q", "--ff-only", "task")
		}, true},
		{"squash with main unchanged", func(t *testing.T, repo, _ string) {
			gitIn(t, repo, "merge", "-q", "--squash", "task")
			gitIn(t, repo, "commit", "-qm", "squash")
		}, true},
		{"squash and main moved on", func(t *testing.T, repo, _ string) {
			gitIn(t, repo, "merge", "-q", "--squash", "task")
			gitIn(t, repo, "commit", "-qm", "squash")
			commitAt(t, repo, "later.txt", "later\n")
		}, true},
		{"one commit of two picked", func(t *testing.T, repo, _ string) {
			gitIn(t, repo, "cherry-pick", "task~1")
		}, false},
		{"other change on main", func(t *testing.T, repo, _ string) {
			commitAt(t, repo, "else.txt", "else\n")
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := landedRepo(t)
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			commitAt(t, repo, "b.txt", "two\n")
			tip := commitAt(t, repo, "c.txt", "three\n")
			gitIn(t, repo, "checkout", "-q", "main")
			tt.land(t, repo, tip)

			landed, err := gitrepo.Landed(context.Background(), repo, tip)
			require.NoError(t, err)
			assert.Equal(t, tt.want, landed)
		})
	}
}

func TestLandedInOriginDefault(t *testing.T) {
	t.Parallel()
	origin := landedRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, origin, "clone", "-q", origin, clone)
	gitIn(t, clone, "config", "user.name", "t")
	gitIn(t, clone, "config", "user.email", "t@t")
	gitIn(t, clone, "checkout", "-q", "-b", "task")
	tip := commitAt(t, clone, "b.txt", "two\n")
	gitIn(t, clone, "push", "-q", "origin", "task")
	gitIn(t, origin, "merge", "-q", "--squash", "task")
	gitIn(t, origin, "commit", "-qm", "squash")
	landed, err := gitrepo.Landed(context.Background(), clone, tip)
	require.NoError(t, err)
	assert.False(t, landed)

	gitIn(t, clone, "fetch", "-q", "origin")

	landed, err = gitrepo.Landed(context.Background(), clone, tip)
	require.NoError(t, err)
	assert.True(t, landed)
}

func TestRepeatedCheckAddsNoObjects(t *testing.T) {
	t.Parallel()
	repo := landedRepo(t)
	gitIn(t, repo, "checkout", "-q", "-b", "task")
	tip := commitAt(t, repo, "b.txt", "two\n")
	gitIn(t, repo, "checkout", "-q", "main")
	commitAt(t, repo, "else.txt", "else\n")
	ctx := context.Background()
	gitrepo.LandedInert(ctx, repo, tip)
	before := gitIn(t, repo, "count-objects")
	time.Sleep(1100 * time.Millisecond)

	gitrepo.LandedInert(ctx, repo, tip)
	_, err := gitrepo.Landed(ctx, repo, tip)

	require.NoError(t, err)
	assert.Equal(t, before, gitIn(t, repo, "count-objects"))
}

func TestFetchOutlivesReads(t *testing.T) {
	t.Parallel()
	origin := landedRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, origin, "clone", "-q", origin, clone)
	slow := filepath.Join(t.TempDir(), "slow-upload-pack")
	require.NoError(t, os.WriteFile(slow, []byte("#!/bin/sh\nsleep 11\nexec git upload-pack \"$@\"\n"), 0o700))
	gitIn(t, clone, "config", "remote.origin.uploadpack", slow)
	tip := commitAt(t, origin, "b.txt", "two\n")

	require.NoError(t, gitrepo.Fetch(context.Background(), clone, "origin"))

	assert.Equal(t, tip, gitIn(t, clone, "rev-parse", "origin/main"))
}

func TestLandedIsExact(t *testing.T) {
	t.Parallel()
	tests := map[string]func(t *testing.T, repo string) string{
		"a whitespace-only difference": func(t *testing.T, repo string) string {
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			tip := commitAt(t, repo, "b.txt", "if  (x) { y }\n")
			gitIn(t, repo, "checkout", "-q", "main")
			commitAt(t, repo, "b.txt", "if (x) { y }\n")
			return tip
		},
		"a tip that is a merge with work of its own": func(t *testing.T, repo string) string {
			gitIn(t, repo, "checkout", "-q", "-b", "other")
			picked := commitAt(t, repo, "c.txt", "c\n")
			gitIn(t, repo, "checkout", "-q", "main")
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			shared := commitAt(t, repo, "b.txt", "b\n")
			gitIn(t, repo, "merge", "-q", "--no-ff", "--no-commit", "other")
			tip := commitAt(t, repo, "only-here.txt", "the merge's own work\n")
			gitIn(t, repo, "checkout", "-q", "main")
			gitIn(t, repo, "cherry-pick", shared)
			gitIn(t, repo, "cherry-pick", picked)
			return tip
		},
		"a squash the default branch reverted": func(t *testing.T, repo string) string {
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			tip := commitAt(t, repo, "b.txt", "b\n")
			gitIn(t, repo, "checkout", "-q", "main")
			gitIn(t, repo, "merge", "-q", "--squash", "task")
			gitIn(t, repo, "commit", "-qm", "squash")
			gitIn(t, repo, "revert", "--no-edit", "HEAD")
			return tip
		},
		"a conflict": func(t *testing.T, repo string) string {
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			tip := commitAt(t, repo, "a.txt", "task\n")
			gitIn(t, repo, "checkout", "-q", "main")
			commitAt(t, repo, "a.txt", "main\n")
			return tip
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := landedRepo(t)
			tip := build(t, repo)

			landed, err := gitrepo.Landed(t.Context(), repo, tip)
			require.NoError(t, err)
			assert.False(t, landed)
			assert.False(t, gitrepo.LandedInert(t.Context(), repo, tip))
		})
	}
}

func TestLandedRunsNoMergeDriverAndTakesNoProofFromOne(t *testing.T) {
	t.Parallel()
	for name, attributes := range map[string]string{".gitattributes": ".gitattributes", "info/attributes": ".git/info/attributes"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := landedRepo(t)
			ran := filepath.Join(t.TempDir(), "ran")
			gitIn(t, repo, "config", "merge.evil.driver", "touch "+ran+"; true")
			require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git", "info"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(repo, attributes), []byte("a.txt merge=evil\n"), 0o600))
			gitIn(t, repo, "add", ".")
			gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "attributes")
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			tip := commitAt(t, repo, "a.txt", "task\n")
			gitIn(t, repo, "checkout", "-q", "main")
			commitAt(t, repo, "a.txt", "main\n")

			assert.False(t, gitrepo.LandedInert(t.Context(), repo, tip))
			assert.NoFileExists(t, ran)
		})
	}
}

func TestLandedNeedsANetChange(t *testing.T) {
	t.Parallel()
	tests := map[string]func(t *testing.T, repo string) string{
		"a commit and its revert": func(t *testing.T, repo string) string {
			commitAt(t, repo, "b.txt", "work\n")
			gitIn(t, repo, "revert", "--no-edit", "HEAD")
			return gitIn(t, repo, "rev-parse", "HEAD")
		},
		"a file added then deleted": func(t *testing.T, repo string) string {
			commitAt(t, repo, "b.txt", "work\n")
			gitIn(t, repo, "rm", "-q", "b.txt")
			gitIn(t, repo, "commit", "-qm", "gone")
			return gitIn(t, repo, "rev-parse", "HEAD")
		},
		"an empty commit": func(t *testing.T, repo string) string {
			gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "empty")
			return gitIn(t, repo, "rev-parse", "HEAD")
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := landedRepo(t)
			gitIn(t, repo, "checkout", "-q", "-b", "task")
			tip := build(t, repo)
			gitIn(t, repo, "checkout", "-q", "main")
			commitAt(t, repo, "later.txt", "later\n")

			landed, err := gitrepo.Landed(t.Context(), repo, tip)
			require.NoError(t, err)
			assert.False(t, landed)
			assert.False(t, gitrepo.LandedInert(t.Context(), repo, tip))
		})
	}
}

func TestLandedTakesNoProofFromConfiguredMerges(t *testing.T) {
	t.Parallel()
	repo := landedRepo(t)
	ran := filepath.Join(t.TempDir(), "ran")
	gitIn(t, repo, "config", "merge.evil.driver", "touch "+ran+"; true")
	gitIn(t, repo, "config", "merge.default", "evil")
	gitIn(t, repo, "checkout", "-q", "-b", "task")
	tip := commitAt(t, repo, "a.txt", "task\n")
	gitIn(t, repo, "checkout", "-q", "main")
	commitAt(t, repo, "a.txt", "main\n")

	landed, err := gitrepo.Landed(t.Context(), repo, tip)
	require.NoError(t, err)
	assert.False(t, landed)
	assert.False(t, gitrepo.LandedInert(t.Context(), repo, tip))
	assert.NoFileExists(t, ran)
}

func TestLandedTakesNoProofFromAReplaceRef(t *testing.T) {
	t.Parallel()
	repo := landedRepo(t)
	gitIn(t, repo, "checkout", "-q", "-b", "task")
	tip := commitAt(t, repo, "b.txt", "work\n")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "replace", tip, gitIn(t, repo, "rev-parse", "main"))

	assert.False(t, gitrepo.LandedInert(t.Context(), repo, tip))
}
