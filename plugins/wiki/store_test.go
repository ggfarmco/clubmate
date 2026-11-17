package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/wikiroot"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wiki-git")
	if err != nil {
		panic(err)
	}
	config := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(config, []byte("[maintenance]\n\tauto = false\n[user]\n\tname = t\n\temail = t@t\n[commit]\n\tgpgsign = false\n"), 0o600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", config)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(tempDir(t), "app")
	writeFile(t, filepath.Join(repo, "pkg/sdui/node.go"), "package sdui\n")
	writeFile(t, filepath.Join(repo, "a.go"), "package a\n")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")
	return repo
}

func newProject(t *testing.T) (wikiroot.Project, string) {
	t.Helper()
	t.Setenv("CLUBMATE_WIKI_DIR", tempDir(t))
	repo := newRepo(t)
	p, err := wikiroot.Ensure(t.Context(), repo)
	require.NoError(t, err)
	return p, repo
}

func commits(t *testing.T, dir string) int {
	t.Helper()
	var n int
	_, err := fmt.Sscan(git(t, dir, "rev-list", "--count", "HEAD"), &n)
	require.NoError(t, err)
	return n - 1
}

func pagesMeta(t *testing.T, dir string) map[string]Meta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	require.NoError(t, err)
	var m map[string]Meta
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func TestWriteReadRoundTrip(t *testing.T) {
	p, repo := newProject(t)
	s := Open(p, time.Now)
	content := "# SDUI components\n\nNodes.\n"

	_, err := s.Write(t.Context(), WriteReq{Page: "sdui/components.md", Content: content, Summary: "explain nodes", By: "Fix SDUI", Worktree: repo, Sources: []string{"pkg/sdui/node.go"}})
	require.NoError(t, err)

	page, got, err := s.Read(t.Context(), "sdui/components.md")
	require.NoError(t, err)
	assert.Equal(t, content, got)
	assert.Equal(t, "Fix SDUI", page.By)
	assert.Equal(t, []string{"pkg/sdui/node.go"}, page.Sources)
	assert.Equal(t, "Fix SDUI|sdui/components.md: explain nodes", git(t, p.Dir, "log", "-1", "--format=%an|%s"))
	assert.NotEmpty(t, pagesMeta(t, p.Dir)["sdui/components.md"].Blobs["pkg/sdui/node.go"])
}

func TestTitleFromHeading(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	for page, content := range map[string]string{
		"a.md":     "intro\n\n# SDUI components\n",
		"b.html":   "<html><head><title>Demo page</title></head></html>",
		"notes.md": "no heading\n",
	} {
		_, err := s.Write(t.Context(), WriteReq{Page: page, Content: content, Summary: "x", By: "you"})
		require.NoError(t, err)
	}

	pages, err := s.List(t.Context(), false)
	require.NoError(t, err)
	titles := map[string]string{}
	for _, pg := range pages {
		titles[pg.Path] = pg.Title
	}
	assert.Equal(t, map[string]string{"a.md": "SDUI components", "b.html": "Demo page", "notes.md": "notes"}, titles)
}

func TestPagePathRejected(t *testing.T) {
	p, _ := newProject(t)
	outside := tempDir(t)
	require.NoError(t, os.Symlink(outside, filepath.Join(p.Dir, "out")))
	s := Open(p, time.Now)

	for page, want := range map[string]string{
		"../x.md":                      "bad page path",
		"/etc/x.md":                    "bad page path",
		"a/../../b.md":                 "bad page path",
		".git/x.md":                    "bad page path",
		"out/x.md":                     "bad page path",
		"x.go":                         "not a page",
		".GIT/x.md":                    "bad page path",
		".Git/hooks/x.md":              "bad page path",
		".git /x.md":                   "bad page path",
		".git./x.md":                   "bad page path",
		"git~1/x.md":                   "bad page path",
		".git::$INDEX_ALLOCATION/x.md": "bad page path",
		".g\u200cit/x.md":              "bad page path",
	} {
		_, err := s.Write(t.Context(), WriteReq{Page: page, Content: "# x\n", Summary: "x", By: "you"})
		assert.EqualError(t, err, want, page)
	}
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(p.Dir), "x.md"))
}

func TestListHidesShadow(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	for _, page := range []string{"a.md", ".agents/trap.md"} {
		_, err := s.Write(t.Context(), WriteReq{Page: page, Content: "# x\n", Summary: "x", By: "you"})
		require.NoError(t, err)
	}

	main, err := s.List(t.Context(), false)
	require.NoError(t, err)
	all, err := s.List(t.Context(), true)
	require.NoError(t, err)

	require.Len(t, main, 1)
	assert.Equal(t, "a.md", main[0].Path)
	require.Len(t, all, 2)
	assert.Equal(t, ".agents/trap.md", all[0].Path)
	assert.True(t, all[0].Shadow)
	assert.False(t, all[1].Shadow)
}

func TestSweepCommitsUserEdits(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "Agent"})
	require.NoError(t, err)
	writeFile(t, filepath.Join(p.Dir, "a.md"), "# A\n\nfixed\n")
	writeFile(t, filepath.Join(p.Dir, "b.md"), "# B\n")
	before := commits(t, p.Dir)

	swept, err := s.Sweep(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"a.md", "b.md"}, swept)

	assert.Equal(t, before+2, commits(t, p.Dir))
	assert.Equal(t, "you|b.md: edited\nyou|a.md: edited", git(t, p.Dir, "log", "-2", "--format=%an|%s"))
	assert.Equal(t, "you", pagesMeta(t, p.Dir)["b.md"].By)
	assert.Empty(t, git(t, p.Dir, "status", "--porcelain"))
}

func TestConcurrentWritersKeepBothCommits(t *testing.T) {
	p, _ := newProject(t)
	stores := []*Store{Open(p, time.Now), Open(p, time.Now)}
	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range 20 {
		wg.Go(func() {
			_, errs[i] = stores[i%2].Write(t.Context(), WriteReq{Page: fmt.Sprintf("p%d.md", i), Content: "# x\n", Summary: "x", By: "you"})
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 20, commits(t, p.Dir))
	assert.Len(t, pagesMeta(t, p.Dir), 20)
	git(t, p.Dir, "fsck", "--no-progress")
}

func TestDeleteCommits(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "you"})
	require.NoError(t, err)

	require.NoError(t, s.Delete(t.Context(), "a.md", "Agent"))

	assert.NoFileExists(t, filepath.Join(p.Dir, "a.md"))
	assert.NotContains(t, pagesMeta(t, p.Dir), "a.md")
	assert.Equal(t, "Agent|a.md: deleted", git(t, p.Dir, "log", "-1", "--format=%an|%s"))
	_, _, err = s.Read(t.Context(), "a.md")
	assert.EqualError(t, err, "not found")
}

func TestTooLarge(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)

	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: strings.Repeat("x", 4<<20+1), Summary: "x", By: "you"})

	assert.EqualError(t, err, "too large")
}

func TestPathspecMagicIsLiteral(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "you"})
	require.NoError(t, err)

	_, err = s.Write(t.Context(), WriteReq{Page: ":(glob)*.md", Content: "# G\n", Summary: "x", By: "you"})
	require.NoError(t, err)

	changes, err := s.History(t.Context(), ":(glob)*.md", 10)
	require.NoError(t, err)
	assert.Len(t, changes, 1)
}

func TestWriteKeepsUserEditInHistory(t *testing.T) {
	p, _ := newProject(t)
	s := Open(p, time.Now)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "first", By: "Agent"})
	require.NoError(t, err)
	writeFile(t, filepath.Join(p.Dir, "a.md"), "# A\n\nuser text\n")

	_, err = s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n\nagent text\n", Summary: "second", By: "Agent"})
	require.NoError(t, err)

	assert.Equal(t, "Agent|a.md: second\nyou|a.md: edited\nAgent|a.md: first", git(t, p.Dir, "log", "-3", "--format=%an|%s"))
}

func TestWriteCommitsGloballyIgnoredPage(t *testing.T) {
	p, _ := newProject(t)
	ignore := filepath.Join(tempDir(t), "ignore")
	writeFile(t, ignore, "*.md\n")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.excludesFile")
	t.Setenv("GIT_CONFIG_VALUE_0", ignore)

	_, err := Open(p, time.Now).Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n", Summary: "x", By: "you"})

	require.NoError(t, err)
	assert.Equal(t, "a.md", git(t, p.Dir, "ls-files", "a.md"))
}

func TestHistoryCountsLinesAndRestoreCommitsOldText(t *testing.T) {
	p, repo := newProject(t)
	s := Open(p, time.Now)
	_, err := s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n\none\ntwo\n", Summary: "first", By: "Agent", Worktree: repo, Sources: []string{"a.go"}})
	require.NoError(t, err)
	_, err = s.Write(t.Context(), WriteReq{Page: "a.md", Content: "# A\n\nthree\n", Summary: "second", By: "Agent", Worktree: repo, Sources: []string{"a.go"}})
	require.NoError(t, err)

	changes, err := s.History(t.Context(), "a.md", historyLimit)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.Equal(t, [2]int{1, 2}, [2]int{changes[0].Added, changes[0].Removed})
	assert.Equal(t, [2]int{4, 0}, [2]int{changes[1].Added, changes[1].Removed})

	_, err = s.Restore(t.Context(), "a.md", changes[1].Rev, userName, "restored")
	require.NoError(t, err)

	_, got, err := s.Read(t.Context(), "a.md")
	require.NoError(t, err)
	assert.Equal(t, "# A\n\none\ntwo\n", got)
	assert.Equal(t, "you|a.md: restored", git(t, p.Dir, "log", "-1", "--format=%an|%s"))
	assert.Equal(t, []string{"a.go"}, pagesMeta(t, p.Dir)["a.md"].Sources)
	_, err = s.Restore(t.Context(), "a.md", "--output=x", userName, "restored")
	assert.ErrorIs(t, err, errNotFound)
}
