package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
)

func TestDiscoverSkipsRetiredPlugin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "graphs")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graphs"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"graphs","name":"Graphs","version":"0.1.0","protocol":1,"command":["graphs"],"contributes":{"commands":[{"id":"graphs.run","title":"Run"}]}}`)
	manifests, errs := plugin.Discover([]string{root})
	assert.Empty(t, errs)
	assert.Empty(t, manifests)
}
