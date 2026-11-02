package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncExtensionsInstallsMissing(t *testing.T) {
	state := t.TempDir()
	log := filepath.Join(t.TempDir(), "installed")
	code := filepath.Join(t.TempDir(), "code")
	writeScript(t, code, "printf 'a.one\\nB.Two\\nc.three\\n'\n")
	writeScript(t, filepath.Join(state, "vscode", "cli", "serve-web", "c1", "bin", "code-server"), `case "$*" in
*--list-extensions*) echo "Extensions installed on this machine:"; echo a.one ;;
*--install-extension*) [ "$2" = c.three ] && { echo "Extension 'c.three' not found." >&2; exit 1; }; echo "$2" >> `+log+` ;;
esac
`)
	s := newVSCodeServer(state, code, &memPorts{})
	var steps []int

	res, err := syncExtensions(context.Background(), s, func(done, total int) { steps = append(steps, done*10+total) })

	require.NoError(t, err)
	assert.Equal(t, []string{"b.two"}, res.Installed)
	assert.Equal(t, []string{"a.one"}, res.Skipped)
	require.Len(t, res.Failed, 1)
	assert.Equal(t, "c.three", res.Failed[0].ID)
	assert.Contains(t, res.Failed[0].Error, "not found")
	raw, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, "b.two", strings.TrimSpace(string(raw)))
	assert.Equal(t, []int{2, 12, 22}, steps)
}

func TestSyncExtensionsNeedsServer(t *testing.T) {
	code := filepath.Join(t.TempDir(), "code")
	writeScript(t, code, "echo a.one\n")
	s := newVSCodeServer(t.TempDir(), code, &memPorts{})

	_, err := syncExtensions(context.Background(), s, func(int, int) {})

	assert.ErrorIs(t, err, errServerMissing)
}
