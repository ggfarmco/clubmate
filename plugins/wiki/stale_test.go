package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/wikiroot"
)

var writtenAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

func gitAt(t *testing.T, dir string, at time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	date := at.Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+date, "GIT_AUTHOR_DATE="+date)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func staleRepo(t *testing.T) (*Store, string, string) {
	t.Helper()
	t.Setenv("CLUBMATE_WIKI_DIR", tempDir(t))
	repo := filepath.Join(tempDir(t), "app")
	writeFile(t, filepath.Join(repo, "a.go"), "package a\n")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	gitAt(t, repo, writtenAt.Add(-24*time.Hour), "commit", "-q", "-m", "init")
	wt := filepath.Join(tempDir(t), "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", wt)
	writeFile(t, filepath.Join(wt, "a.go"), "package a\n\nfunc A() {}\n")
	gitAt(t, wt, writtenAt.Add(-time.Hour), "commit", "-q", "-am", "feat")
	proj, err := wikiroot.Ensure(t.Context(), wt)
	require.NoError(t, err)
	s := Open(proj, func() time.Time { return writtenAt })
	_, err = s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "Agent", Worktree: wt, Sources: []string{"a.go"}})
	require.NoError(t, err)
	return s, repo, wt
}

func stale(t *testing.T, s *Store) bool {
	t.Helper()
	page, _, err := s.Read(t.Context(), "a.md")
	require.NoError(t, err)
	return page.Stale
}

func TestStaleAcrossSquashLanding(t *testing.T) {
	s, repo, _ := staleRepo(t)
	assert.False(t, stale(t, s))

	git(t, repo, "merge", "-q", "--squash", "feat")
	gitAt(t, repo, writtenAt.Add(time.Hour), "commit", "-q", "-m", "land")

	assert.False(t, stale(t, s))
}

func TestStaleAfterLaterChange(t *testing.T) {
	s, repo, _ := staleRepo(t)
	git(t, repo, "merge", "-q", "--squash", "feat")
	gitAt(t, repo, writtenAt.Add(time.Hour), "commit", "-q", "-m", "land")
	writeFile(t, filepath.Join(repo, "a.go"), "package a\n\nfunc B() {}\n")
	gitAt(t, repo, writtenAt.Add(2*time.Hour), "commit", "-q", "-am", "later")

	assert.True(t, stale(t, s))
}

func TestStaleDeletedSource(t *testing.T) {
	s, repo, _ := staleRepo(t)
	git(t, repo, "rm", "-q", "a.go")
	gitAt(t, repo, writtenAt.Add(time.Hour), "commit", "-q", "-m", "rm")

	assert.True(t, stale(t, s))
}

func TestNoSourcesNeverStale(t *testing.T) {
	s, repo, _ := staleRepo(t)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "Agent", Worktree: repo})
	require.NoError(t, err)
	writeFile(t, filepath.Join(repo, "a.go"), "package a\n\nfunc B() {}\n")
	gitAt(t, repo, writtenAt.Add(2*time.Hour), "commit", "-q", "-am", "later")

	assert.False(t, stale(t, s))
}

func TestListStaleUsesEarliestWriteOfSharedSource(t *testing.T) {
	s, repo, _ := staleRepo(t)
	proj, ok, err := wikiroot.Find(t.Context(), repo)
	require.NoError(t, err)
	require.True(t, ok)
	older := Open(proj, func() time.Time { return writtenAt.Add(-12 * time.Hour) })
	_, err = older.Write(t.Context(), WriteReq{Page: "b.md", Content: "# B\n", Summary: "x", By: "Agent", Worktree: repo, Sources: []string{"a.go"}})
	require.NoError(t, err)
	writeFile(t, filepath.Join(repo, "a.go"), "package a\n\nfunc B() {}\n")
	gitAt(t, repo, writtenAt.Add(-6*time.Hour), "commit", "-q", "-am", "between")

	pages, err := s.List(t.Context(), false)

	require.NoError(t, err)
	got := map[string]bool{}
	for _, p := range pages {
		got[p.Path] = p.Stale
	}
	assert.Equal(t, map[string]bool{"a.md": false, "b.md": true}, got)
}

func countGit(t *testing.T) func() int {
	t.Helper()
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	dir := tempDir(t)
	log := filepath.Join(dir, "calls")
	writeFile(t, filepath.Join(dir, "git"), "#!/bin/sh\necho x >> '"+log+"'\nexec '"+real+"' \"$@\"\n")
	require.NoError(t, os.Chmod(filepath.Join(dir, "git"), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return 0
		}
		require.NoError(t, err)
		return strings.Count(string(data), "\n")
	}
}

func TestListRunsBoundedGit(t *testing.T) {
	s, repo, wt := staleRepo(t)
	for i := range 40 {
		var sources []string
		for j := range 3 {
			src := fmt.Sprintf("pkg%d/f%d.go", i, j)
			writeFile(t, filepath.Join(wt, src), "package p\n")
			writeFile(t, filepath.Join(repo, src), "package p\n\nvar X = 1\n")
			sources = append(sources, src)
		}
		_, err := s.Write(t.Context(), WriteReq{Page: fmt.Sprintf("p%d.md", i), Content: "# P\n", Summary: "x", By: "Agent", Worktree: wt, Sources: sources})
		require.NoError(t, err)
	}
	git(t, repo, "add", ".")
	gitAt(t, repo, writtenAt.Add(time.Hour), "commit", "-q", "-m", "later")
	calls := countGit(t)

	pages, err := s.List(t.Context(), false)

	require.NoError(t, err)
	require.Len(t, pages, 41)
	for _, p := range pages {
		assert.Equal(t, p.Path != "a.md", p.Stale, p.Path)
	}
	assert.LessOrEqual(t, calls(), 10)
}
