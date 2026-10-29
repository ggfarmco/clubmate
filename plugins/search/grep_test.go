package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrepFindsWithOptions(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "a.go", "Foo bar\nfoobar\nfoo\n")
	write(t, dir, "b.txt", "foo\n")
	tests := []struct {
		name string
		opts grepOptions
		want []string
	}{
		{name: "insensitive", opts: grepOptions{Query: "foo"}, want: []string{"a.go:1", "a.go:2", "a.go:3", "b.txt:1"}},
		{name: "case", opts: grepOptions{Query: "Foo", Case: true}, want: []string{"a.go:1"}},
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

			s := newSearches()
			id := s.start("w", prepared(eng), grepOptions{Query: "hit"})
			first := s.poll(t.Context(), id, 0)
			s.cancel(id)
			after := s.poll(t.Context(), id, first.Next)
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
			old := s.poll(t.Context(), a, 0)
			assert.True(t, old.Done)
			assert.Empty(t, old.Matches)
			cur := s.poll(t.Context(), b, 0)
			assert.Equal(t, b, cur.ID)
			assert.NotEmpty(t, cur.Matches)
		})
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

func prepared(eng engine) func(context.Context) (engine, error) {
	return func(context.Context) (engine, error) { return eng, nil }
}

func drain(t *testing.T, s *searches, eng engine, opts grepOptions) ([]grepMatch, pollResult) {
	t.Helper()
	id := s.start("w", prepared(eng), opts)
	var all []grepMatch
	deadline := time.Now().Add(10 * time.Second)
	for after := 0; ; {
		require.True(t, time.Now().Before(deadline))
		res := s.poll(t.Context(), id, after)
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
