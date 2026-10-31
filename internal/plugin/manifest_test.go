package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
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

func TestLoadAcceptsLegacyProjectScope(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"sections":[{"id":"beta","title":"Beta","scope":"project"}],"commands":[{"id":"beta.go","title":"Go","scope":"project"}]}}`)
	m, err := plugin.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "workspace", m.Contributes.Sections[0].Scope)
	assert.Equal(t, "workspace", m.Contributes.Commands[0].Scope)
}

func TestLoadRejectsReservedID(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"workspace","name":"W","version":"0.1.0","protocol":1,"command":["run"]}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestManifestMetadataOptional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go"}]}}`)
	m, err := plugin.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, []rpc.CommandDef{{ID: "beta.go", Title: "Go"}}, m.Contributes.Commands)
}

func TestLoadRejectsInvalidArgsSchema(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go","args":{"properties":{"x":{"type":"object"}}}}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestLoadRejectsSelfHostedSection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"sections":[{"id":"beta","title":"Beta","host":"beta"}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestLoadRejectsInvalidExtraSlot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"sections":[{"id":"beta","title":"Beta","slots":["main"]}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestLoadRejectsCommandTimeoutOutOfRange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go","timeout":601}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
}

func TestLoadChecksCommandIDStyle(t *testing.T) {
	tests := []struct {
		id string
		ok bool
	}{
		{id: "beta.worktree-new", ok: true},
		{id: "beta.worktree.set", ok: true},
		{id: "beta.setPort"},
		{id: "beta.a.b.c"},
		{id: "beta.kill--orphans"},
		{id: "beta"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
			writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"`+tt.id+`","title":"Go"}]}}`)
			_, err := plugin.Load(dir)
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, plugin.ErrInvalidManifest)
			assert.ErrorContains(t, err, `command id "`+tt.id+`" must be beta.<noun>[.<verb>] in kebab-case`)
		})
	}
}
