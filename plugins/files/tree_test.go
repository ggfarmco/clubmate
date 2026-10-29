package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func labels(items []sdui.TreeItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func TestBuildTree(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"b", "big", ".git", "-d é"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, d), 0o700))
	}
	for _, f := range []string{"A.txt", ".DS_Store"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), nil, 0o600))
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "b"), filepath.Join(root, "link")))
	for i := range 1002 {
		require.NoError(t, os.WriteFile(filepath.Join(root, "big", fmt.Sprintf("f%04d", i)), nil, 0o600))
	}
	status := map[string]string{filepath.Join(root, "b", "f"): "M", filepath.Join(root, "A.txt"): "U"}

	items := buildTree([]Root{{Path: root}}, map[string]bool{root: true, filepath.Join(root, "big"): true}, status, false)

	require.Len(t, items, 1)
	top := items[0]
	assert.Equal(t, root, top.ID)
	assert.True(t, top.HasChildren)
	assert.Equal(t, "•", top.Badge)
	assert.Equal(t, []string{"-d é", "b", "big", "link", "A.txt"}, labels(top.Children))
	byLabel := map[string]sdui.TreeItem{}
	for _, c := range top.Children {
		byLabel[c.Label] = c
	}
	assert.False(t, byLabel["link"].HasChildren)
	assert.Equal(t, "link", byLabel["link"].Icon)
	assert.Equal(t, "•", byLabel["b"].Badge)
	assert.Equal(t, "U", byLabel["A.txt"].Badge)
	big := byLabel["big"].Children
	require.Len(t, big, 1001)
	assert.Equal(t, "… 2 more", big[1000].Label)
	assert.Equal(t, filepath.Join(root, "big")+"/\x00more", big[1000].ID)
}

func TestMissingRepoRoot(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	items := buildTree([]Root{{Path: gone}}, map[string]bool{gone: true}, nil, false)
	require.Len(t, items, 1)
	assert.Equal(t, "missing", items[0].Detail)
	assert.False(t, items[0].HasChildren)
	assert.Empty(t, items[0].Children)
}

func countItems(items []sdui.TreeItem) int {
	n := len(items)
	for _, it := range items {
		n += countItems(it.Children)
	}
	return n
}

func TestTreeCapPassesValidation(t *testing.T) {
	root := t.TempDir()
	expanded := map[string]bool{root: true}
	dir := root
	for range 8 {
		for i := range 999 {
			require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d", i)), nil, 0o600))
		}
		dir = filepath.Join(dir, "a")
		require.NoError(t, os.Mkdir(dir, 0o700))
		expanded[dir] = true
	}
	items := buildTree([]Root{{Path: root}}, expanded, nil, false)
	assert.LessOrEqual(t, countItems(items), 4950)
	assert.GreaterOrEqual(t, countItems(items), 4800)
	require.NoError(t, internalsdui.Validate(treeView(items, "", nil)))
}

func TestIgnoredMuted(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "bin", "sub"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin", "out"), nil, 0o600))
	status := map[string]string{filepath.Join(root, "bin"): "!"}
	expanded := map[string]bool{root: true, filepath.Join(root, "bin"): true}

	top := buildTree([]Root{{Path: root}}, expanded, status, false)[0]
	assert.Empty(t, top.Badge)
	require.Equal(t, []string{"bin", "keep.go"}, labels(top.Children))
	bin := top.Children[0]
	assert.True(t, bin.Muted)
	assert.Empty(t, bin.Badge)
	require.Len(t, bin.Children, 2)
	assert.True(t, bin.Children[0].Muted)
	assert.False(t, top.Children[1].Muted)

	top = buildTree([]Root{{Path: root}}, expanded, status, true)[0]
	assert.Equal(t, []string{"keep.go"}, labels(top.Children))
}

func TestCompactChain(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(deep, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(deep, "x.go"), nil, 0o600))
	status := map[string]string{filepath.Join(deep, "x.go"): "M"}

	top := buildTree([]Root{{Path: root}}, map[string]bool{root: true}, status, false)[0]
	require.Len(t, top.Children, 1)
	row := top.Children[0]
	assert.Equal(t, "a / b / c", row.Label)
	assert.Equal(t, deep, row.ID)
	assert.Equal(t, "•", row.Badge)
	assert.Equal(t, "folder", row.Icon)

	top = buildTree([]Root{{Path: root}}, map[string]bool{root: true, deep: true}, status, false)[0]
	file := top.Children[0].Children
	require.Len(t, file, 1)
	assert.Equal(t, "x.go", file[0].Label)
	assert.Empty(t, file[0].Icon)
}

func TestHomeChildrenNotCompacted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "Documents", "inner"), 0o700))

	top := buildTree([]Root{{Path: home}}, map[string]bool{home: true}, nil, false)[0]

	assert.Equal(t, []string{"Documents"}, labels(top.Children))
}

func TestCompactChainSplits(t *testing.T) {
	root := t.TempDir()
	ab := filepath.Join(root, "a", "b")
	deep := filepath.Join(ab, "c")
	require.NoError(t, os.MkdirAll(deep, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ab, "y.go"), nil, 0o600))
	expanded := map[string]bool{root: true, deep: true}

	top := buildTree([]Root{{Path: root}}, expanded, nil, false)[0]
	require.Len(t, top.Children, 1)
	row := top.Children[0]
	assert.Equal(t, "a / b", row.Label)
	assert.Equal(t, ab, row.ID)
	assert.True(t, row.Expanded)
	require.Equal(t, []string{"c", "y.go"}, labels(row.Children))
	assert.True(t, row.Children[0].Expanded)

	require.NoError(t, os.Remove(filepath.Join(ab, "y.go")))
	top = buildTree([]Root{{Path: root}}, map[string]bool{root: true, filepath.Join(root, "a"): true, ab: true}, nil, false)[0]
	row = top.Children[0]
	assert.Equal(t, "a / b / c", row.Label)
	assert.True(t, row.Expanded)
}

func TestCompactChainBadge(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "b", "x.go"), nil, 0o600))
	status := map[string]string{filepath.Join(root, "a"): "U"}

	top := buildTree([]Root{{Path: root}}, map[string]bool{root: true}, status, false)[0]
	assert.Equal(t, "U", top.Children[0].Badge)
}

func TestCollapseForgetsChain(t *testing.T) {
	root := t.TempDir()
	a, ab, abc := filepath.Join(root, "a"), filepath.Join(root, "a", "b"), filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(abc, 0o700))
	s := &state{repos: []string{root}, expanded: map[string]bool{root: true, a: true, ab: true, abc: true}}

	s.collapseLocked(abc)

	assert.Equal(t, map[string]bool{root: true}, s.expanded)
}
