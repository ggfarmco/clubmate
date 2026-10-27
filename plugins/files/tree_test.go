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

	items := buildTree([]Root{{Path: root}}, map[string]bool{root: true, filepath.Join(root, "big"): true}, status)

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
	items := buildTree([]Root{{Path: gone}}, map[string]bool{gone: true}, nil)
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
	items := buildTree([]Root{{Path: root}}, expanded, nil)
	assert.LessOrEqual(t, countItems(items), 4950)
	assert.GreaterOrEqual(t, countItems(items), 4800)
	require.NoError(t, internalsdui.Validate(treeView(items, "")))
}
