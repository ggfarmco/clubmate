package main_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type foundRepos struct {
	Repo  bool `json:"repo"`
	Repos []struct {
		Path       string    `json:"path"`
		Name       string    `json:"name"`
		LastCommit time.Time `json:"lastCommit"`
	} `json:"repos"`
	Partial bool `json:"partial"`
}

func repoAt(t *testing.T, dir, date string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	git(t, dir, "init", "-q", "-b", "main")
	if date == "" {
		return
	}
	cmd := []string{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c", "--date", date}
	t.Setenv("GIT_COMMITTER_DATE", date)
	git(t, dir, cmd...)
}

func TestReposInFindsReposTwoLevelsDownNewestFirst(t *testing.T) {
	h, id := open(t, committedRepo(t))
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repoAt(t, filepath.Join(parent, "old"), "2026-01-01T10:00:00Z")
	repoAt(t, filepath.Join(parent, "new"), "2026-11-01T10:00:00Z")
	repoAt(t, filepath.Join(parent, "group", "deep"), "2026-06-01T10:00:00Z")
	repoAt(t, filepath.Join(parent, "empty"), "")
	repoAt(t, filepath.Join(parent, "new", "inner"), "2026-11-02T10:00:00Z")
	repoAt(t, filepath.Join(parent, "a", "b", "too-deep"), "2026-11-03T10:00:00Z")
	repoAt(t, filepath.Join(parent, ".hidden"), "2026-11-04T10:00:00Z")
	repoAt(t, filepath.Join(parent, "node_modules", "pkg"), "2026-11-05T10:00:00Z")
	repoAt(t, filepath.Join(parent, "web", "node_modules"), "2026-11-06T10:00:00Z")

	var got foundRepos
	require.NoError(t, runCommand(t, h, id, "git.repos-in", map[string]any{"path": parent, "dates": true}, &got))

	var names []string
	for _, r := range got.Repos {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"new", "group/deep", "old", "empty"}, names)
	assert.Equal(t, filepath.Join(parent, "group", "deep"), got.Repos[1].Path)
	assert.False(t, got.Repo)
	assert.False(t, got.Partial)

	var inside foundRepos
	require.NoError(t, runCommand(t, h, id, "git.repos-in", map[string]any{"path": filepath.Join(parent, "new")}, &inside))
	assert.True(t, inside.Repo)
	assert.Empty(t, inside.Repos)
}
