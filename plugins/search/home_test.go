package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkSkipsHomeChildren(t *testing.T) {
	parent := tempDir(t)
	home := filepath.Join(parent, "home")
	write(t, home, "top.txt", "")
	write(t, home, "Documents/d.txt", "")
	write(t, home, "Library/l.txt", "")
	write(t, parent, "other/o.txt", "")

	got, err := index{home: home}.Files(t.Context(), []string{parent})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"home/top.txt", "other/o.txt"}, got)
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
