package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeldFilesListBinariesByPathOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := [][2]string{
		{"plan.md", "the plan"},
		{"nul.bin", "abc\x00def"},
		{"bad.bin", "abc\xff\xfedef"},
		{"tail.bin", "abc\xe2\x80"},
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		p := filepath.Join(dir, f[0])
		require.NoError(t, os.WriteFile(p, []byte(f[1]), 0o600))
		paths = append(paths, p)
	}

	out := heldFiles(paths)

	assert.Contains(t, out, "--- "+filepath.Join(dir, "plan.md")+"\nthe plan\n")
	assert.Contains(t, out, "--- "+filepath.Join(dir, "nul.bin")+" (binary, not shown)\n")
	assert.Contains(t, out, "--- "+filepath.Join(dir, "bad.bin")+" (binary, not shown)\n")
	assert.Contains(t, out, "--- "+filepath.Join(dir, "tail.bin")+" (binary, not shown)\n")
	assert.NotContains(t, out, "abc")
}

func TestHeldFilesCutTextAtARuneBoundary(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "long.txt")
	require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("—", heldFilesLimit)), 0o600))

	out := heldFiles([]string{p})

	assert.Contains(t, out, "--- "+p+"\n——")
	assert.Contains(t, out, heldFilesCut)
	assert.True(t, utf8.ValidString(out))
}
