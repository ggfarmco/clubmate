package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
)

func TestLoadRejectsCommandOutsideDir(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"id":"bad","name":"Bad","version":"0.1.0","protocol":1,"command":["../../bin/sh"]}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o600))
}

func TestLoadRejectsInvalidViewSlot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"views":[{"slot":"alpha.footer"}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestLoadRejectsInvalidSectionScope(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"sections":[{"id":"beta","title":"Beta","scope":"global"}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestManifestMetadataOptional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go"}]}}`)
	m, err := plugin.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, []plugin.CommandDef{{ID: "beta.go", Title: "Go"}}, m.Contributes.Commands)
}

func TestLoadRejectsInvalidArgsSchema(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go","args":{"properties":{"x":{"type":"object"}}}}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}
