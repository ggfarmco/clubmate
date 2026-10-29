package main

import (
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
