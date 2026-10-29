package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestGrepFindsWithOptions(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "a.go", "Foo bar\nfoobar\nfoo\n")
	write(t, dir, "b.txt", "foo\n")
	outside := tempDir(t)
	write(t, outside, "secret.txt", "foo\n")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")))
	write(t, outside, "rgconfig", "--smart-case\n--glob=*.none\n")
	t.Setenv("RIPGREP_CONFIG_PATH", filepath.Join(outside, "rgconfig"))
	tests := []struct {
		name string
		opts grepOptions
		want []string
	}{
		{name: "insensitive", opts: grepOptions{Query: "foo"}, want: []string{"a.go:1", "a.go:2", "a.go:3", "b.txt:1"}},
		{name: "case", opts: grepOptions{Query: "foo", Case: true}, want: []string{"a.go:2", "a.go:3", "b.txt:1"}},
		{name: "word", opts: grepOptions{Query: "foo", Word: true}, want: []string{"a.go:1", "a.go:3", "b.txt:1"}},
		{name: "regex", opts: grepOptions{Query: "fo+b", Regex: true}, want: []string{"a.go:2"}},
		{name: "literal", opts: grepOptions{Query: "fo+b"}, want: nil},
		{name: "glob", opts: grepOptions{Query: "foo", Glob: "*.go"}, want: []string{"a.go:1", "a.go:2", "a.go:3"}},
	}
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					got, _ := drain(t, newSearches(), eng, tt.opts)
					var lines []string
					for _, m := range got {
						lines = append(lines, m.Path+":"+strconv.Itoa(m.Line))
					}
					assert.ElementsMatch(t, tt.want, lines)
				})
			}
			got, _ := drain(t, newSearches(), eng, grepOptions{Query: "BAR"})
			require.Len(t, got, 2)
			for _, m := range got {
				if m.Line == 1 {
					assert.Equal(t, grepMatch{Path: "a.go", AbsPath: filepath.Join(dir, "a.go"), Line: 1, Col: 5, Text: "Foo bar", Ranges: [][2]int{{4, 7}}}, m)
				}
			}
		})
	}
}

func TestGrepSnippetsAndBinaries(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "long.txt", "é"+strings.Repeat("x", 9000)+"NEEDLE"+strings.Repeat("y", 1000)+"\n")
	write(t, dir, "bin.dat", "abc\x00NEEDLE\n")
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			got, _ := drain(t, newSearches(), eng, grepOptions{Query: "needle"})
			require.Len(t, got, 1)
			m := got[0]
			assert.Equal(t, "long.txt", m.Path)
			assert.Equal(t, 9003, m.Col)
			text := []rune(m.Text)
			assert.LessOrEqual(t, len(text), snippetRunes)
			require.Len(t, m.Ranges, 1)
			assert.Equal(t, "NEEDLE", string(text[m.Ranges[0][0]:m.Ranges[0][1]]))
		})
	}
}

func TestGrepCapsAndCancels(t *testing.T) {
	dir := tempDir(t)
	for i := range 25 {
		write(t, dir, fmt.Sprintf("f%02d.txt", i), strings.Repeat("hit\n", 800))
	}
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			got, last := drain(t, newSearches(), eng, grepOptions{Query: "hit"})
			assert.Len(t, got, maxMatches)
			assert.True(t, last.Truncated)

			g := &gated{engine: eng, first: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
			s := newSearches()
			id := s.start("w", prepared(g), grepOptions{Query: "hit"})
			<-g.first
			s.cancel("w", id)
			close(g.release)
			select {
			case <-g.finished:
			case <-time.After(5 * time.Second):
				require.Fail(t, "engine kept running after cancel")
			}
			assert.Equal(t, int32(1), g.calls.Load())
			after := s.poll(t.Context(), "w", id, 0)
			assert.True(t, after.Done)
			assert.Empty(t, after.Matches)
		})
	}
}

func TestNewSearchCancelsOld(t *testing.T) {
	dir := tempDir(t)
	for i := range 25 {
		write(t, dir, fmt.Sprintf("f%02d.txt", i), strings.Repeat("hit\n", 800))
	}
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			s := newSearches()
			a := s.start("w", prepared(eng), grepOptions{Query: "hit"})
			b := s.start("w", prepared(eng), grepOptions{Query: "hit"})
			old := s.poll(t.Context(), "w", a, 0)
			assert.True(t, old.Done)
			assert.Empty(t, old.Matches)
			cur := s.poll(t.Context(), "w", b, 0)
			assert.Equal(t, b, cur.ID)
			assert.NotEmpty(t, cur.Matches)
			assert.True(t, s.poll(t.Context(), "other", b, 0).Done)
		})
	}
}

func TestSearchGrepLimit(t *testing.T) {
	dir := tempDir(t)
	for i := range 64 {
		write(t, dir, fmt.Sprintf("f%02d.txt", i), "hit\n")
	}
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			all, truncated, err := collect(t.Context(), eng, grepOptions{Query: "hit"}, 100)
			require.NoError(t, err)
			assert.Len(t, all, 64)
			assert.False(t, truncated)

			some, truncated, err := collect(t.Context(), eng, grepOptions{Query: "hit"}, 10)
			require.NoError(t, err)
			assert.Len(t, some, 10)
			assert.True(t, truncated)
		})
	}
}

func TestGrepUnreadableDirIsNoError(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "a.txt", "text\n")
	write(t, dir, "locked/b.txt", "text\n")
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			got, last := drain(t, newSearches(), eng, grepOptions{Query: "absent"})
			assert.Empty(t, got)
			assert.Empty(t, last.Error)
		})
	}
}

func TestGrepNarrowGlobIsBounded(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "keep.go", "hit\n")
	for i := range 150 {
		write(t, dir, fmt.Sprintf("a%03d.txt", i), strings.Repeat("hit\n", 1000))
	}
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			begin := time.Now()
			got, last := drain(t, newSearches(), eng, grepOptions{Query: "hit", Glob: "*.go"})
			assert.Less(t, time.Since(begin), 2*time.Second)
			require.Len(t, got, 1)
			assert.Equal(t, "keep.go", got[0].Path)
			assert.False(t, last.Truncated)
		})
	}
	for i := range 100 {
		write(t, dir, fmt.Sprintf("b%03d.txt", i), strings.Repeat("hit\n", 1000))
	}
	if eng, ok := engines(t, dir)["rg"]; ok {
		begin := time.Now()
		_, last := drain(t, newSearches(), eng, grepOptions{Query: "hit", Glob: "*.go"})
		assert.Less(t, time.Since(begin), 2*time.Second)
		assert.True(t, last.Truncated)
	}
}

func TestGrepSkipsSymlinkedDirs(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, outside := tempDir(t), tempDir(t)
	write(t, dir, "a/b.txt", "hit\n")
	write(t, dir, "c.txt", "hit\n")
	write(t, outside, "b.txt", "hit\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "a")))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "a")))
	for name, eng := range engines(t, dir) {
		t.Run(name, func(t *testing.T) {
			got, _ := drain(t, newSearches(), eng, grepOptions{Query: "hit"})
			require.Len(t, got, 1)
			assert.Equal(t, "c.txt", got[0].Path)
		})
	}
}

func TestNestedFoldersListedOnce(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := tempDir(t)
	write(t, repo, ".gitignore", ".worktrees/\n")
	write(t, repo, "a.txt", "hit\n")
	worktree := filepath.Join(repo, ".worktrees", "x")
	write(t, worktree, "b.txt", "hit\n")
	plain := tempDir(t)
	write(t, plain, "c.txt", "hit\n")
	write(t, plain, "sub/d.txt", "hit\n")
	for _, dir := range []string{repo, worktree} {
		out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput()
		require.NoError(t, err, string(out))
	}
	tests := []struct {
		name    string
		folders []string
		want    []string
	}{
		{name: "ignored worktree", folders: []string{repo, worktree}, want: []string{filepath.Join(repo, "a.txt"), filepath.Join(worktree, "b.txt")}},
		{name: "plain nested", folders: []string{plain, filepath.Join(plain, "sub")}, want: []string{filepath.Join(plain, "c.txt"), filepath.Join(plain, "sub", "d.txt")}},
	}
	for _, tt := range tests {
		for name, eng := range engines(t, tt.folders...) {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				got, _ := drain(t, newSearches(), eng, grepOptions{Query: "hit"})
				var files []string
				for _, m := range got {
					files = append(files, m.AbsPath)
				}
				assert.ElementsMatch(t, tt.want, files)
			})
		}
	}
}

func TestPreview(t *testing.T) {
	dir := tempDir(t)
	var b strings.Builder
	for n := 1; n <= 100; n++ {
		if n == 49 {
			b.WriteString(strings.Repeat("ж", 2000) + "\n")
			continue
		}
		fmt.Fprintf(&b, "line %d\n", n)
	}
	write(t, dir, "f.txt", b.String())

	got, err := preview(filepath.Join(dir, "f.txt"), 50, 2)

	require.NoError(t, err)
	assert.Equal(t, []previewLine{
		{N: 48, Text: "line 48"},
		{N: 49, Text: strings.Repeat("ж", snippetRunes)},
		{N: 50, Text: "line 50"},
		{N: 51, Text: "line 51"},
		{N: 52, Text: "line 52"},
	}, got)
}

func TestSearchUsesBundledRg(t *testing.T) {
	res := tempDir(t)
	write(t, res, "tools/rg", "#!/bin/sh\ntouch \"$0.ran\"\n")
	require.NoError(t, os.Chmod(filepath.Join(res, "tools", "rg"), 0o700))
	dir := tempDir(t)
	write(t, dir, "a.txt", "foo\n")
	project := pluginsdk.Project{Roots: []string{dir}}
	a := &app{}

	t.Setenv("CLUBMATE_RESOURCES", res)
	eng, err := a.engine(t.Context(), nil, project, func() {})
	require.NoError(t, err)
	require.Equal(t, rgEngine{bin: filepath.Join(res, "tools", "rg"), roots: newRoots(project.Roots)}, eng)
	require.NoError(t, eng.run(t.Context(), grepOptions{Query: "foo"}, func([]grepMatch) bool { return true }))
	assert.FileExists(t, filepath.Join(res, "tools", "rg.ran"))

	t.Setenv("CLUBMATE_RESOURCES", tempDir(t))
	want, lookErr := exec.LookPath("rg")
	if lookErr != nil {
		t.Skip("rg not on PATH")
	}
	eng, err = a.engine(t.Context(), nil, project, func() {})
	require.NoError(t, err)
	assert.Equal(t, want, eng.(rgEngine).bin)
}

func engines(t *testing.T, folders ...string) map[string]engine {
	t.Helper()
	paths, err := index{}.Files(t.Context(), folders)
	require.NoError(t, err)
	out := map[string]engine{"builtin": builtinEngine{list: newListing(folders, paths)}}
	if bin, err := exec.LookPath("rg"); err == nil {
		out["rg"] = rgEngine{bin: bin, roots: newRoots(folders)}
	}
	return out
}

type gated struct {
	engine
	first    chan struct{}
	release  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
}

func (g *gated) run(ctx context.Context, opts grepOptions, emit serialEmit) error {
	defer close(g.finished)
	return g.engine.run(ctx, opts, func(batch []grepMatch) bool {
		if g.calls.Add(1) == 1 {
			close(g.first)
			<-g.release
		}
		return emit(batch)
	})
}

func prepared(eng engine) func(context.Context, func()) (engine, error) {
	return func(context.Context, func()) (engine, error) { return eng, nil }
}

func drain(t *testing.T, s *searches, eng engine, opts grepOptions) ([]grepMatch, pollResult) {
	t.Helper()
	id := s.start("w", prepared(eng), opts)
	var all []grepMatch
	deadline := time.Now().Add(10 * time.Second)
	for after := 0; ; {
		require.True(t, time.Now().Before(deadline))
		res := s.poll(t.Context(), "w", id, after)
		require.Empty(t, res.Error)
		assert.LessOrEqual(t, len(res.Matches), pollBatch)
		all = append(all, res.Matches...)
		after = res.Next
		if res.Done {
			return all, res
		}
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
