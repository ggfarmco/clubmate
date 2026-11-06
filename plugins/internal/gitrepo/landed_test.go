package gitrepo_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

			assert.Equal(t, tt.want, gitrepo.Landed(context.Background(), repo, tip))
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
	assert.False(t, gitrepo.Landed(context.Background(), clone, tip))

	gitIn(t, clone, "fetch", "-q", "origin")

	assert.True(t, gitrepo.Landed(context.Background(), clone, tip))
}
