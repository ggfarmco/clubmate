package plugin_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestLoadRejectsProjectScope(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go","scope":"project"}]}}`)
	_, err := plugin.Load(dir)
	assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
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
	writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","icon":"mic","protocol":1,"command":["run"],"contributes":{"commands":[{"id":"beta.go","title":"Go"}]}}`)
	m, err := plugin.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, []rpc.CommandDef{{ID: "beta.go", Title: "Go"}}, m.Contributes.Commands)
	assert.Equal(t, "mic", m.Icon)
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
	for _, slot := range []string{"main", "settings"} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
		writeManifest(t, dir, fmt.Sprintf(`{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"sections":[{"id":"beta","title":"Beta","slots":[%q]}]}}`, slot))
		_, err := plugin.Load(dir)
		assert.ErrorIs(t, err, plugin.ErrInvalidManifest, slot)
	}
}

func TestLoadSettingsSlotOnlyForOwnID(t *testing.T) {
	for slot, ok := range map[string]bool{"beta.settings": true, "alpha.settings": false} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
		writeManifest(t, dir, fmt.Sprintf(`{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"views":[{"slot":%q}]}}`, slot))
		_, err := plugin.Load(dir)
		if ok {
			assert.NoError(t, err, slot)
		} else {
			assert.ErrorIs(t, err, plugin.ErrInvalidManifest, slot)
		}
	}
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

func TestLoadChecksShortcuts(t *testing.T) {
	tests := []struct {
		name      string
		shortcuts string
		ok        bool
	}{
		{name: "valid", shortcuts: `[{"keys":"cmd+shift+n","action":"New task","when":"list focused"},{"keys":"p","action":"Pause"}]`, ok: true},
		{name: "bad combo", shortcuts: `[{"keys":"hyper+r","action":"Run"}]`},
		{name: "reserved bare key", shortcuts: `[{"keys":"return","action":"Open"}]`},
		{name: "no action", shortcuts: `[{"keys":"cmd+r","action":" "}]`},
		{name: "duplicate keys", shortcuts: `[{"keys":"cmd+r","action":"Run"},{"keys":"cmd+r","action":"Rerun"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o700))
			writeManifest(t, dir, `{"id":"beta","name":"Beta","version":"0.1.0","protocol":1,"command":["run"],"contributes":{"shortcuts":`+tt.shortcuts+`}}`)
			m, err := plugin.Load(dir)
			if !tt.ok {
				assert.ErrorIs(t, err, plugin.ErrInvalidManifest)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, plugin.Shortcut{Keys: "cmd+shift+n", Action: "New task", When: "list focused"}, m.Contributes.Shortcuts[0])
		})
	}
}

func TestBundledShortcutsAreInDocs(t *testing.T) {
	doc, err := os.ReadFile("../../docs/shortcuts.md")
	require.NoError(t, err)
	documented := docKeys(string(doc))
	paths, err := filepath.Glob("../../plugins/*/plugin.json")
	require.NoError(t, err)
	for _, path := range paths {
		m := loadBundled(t, path)
		for _, s := range m.Contributes.Shortcuts {
			assert.True(t, documented[keyLabel(s.Keys)], "%s: %s (%s) is not in docs/shortcuts.md", m.ID, s.Keys, keyLabel(s.Keys))
		}
	}
}

func loadBundled(t *testing.T, path string) plugin.Manifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var m plugin.Manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	dir := t.TempDir()
	require.NotEmpty(t, m.Command, path)
	require.NoError(t, os.WriteFile(filepath.Join(dir, m.Command[0]), []byte("#!/bin/sh\n"), 0o700))
	writeManifest(t, dir, string(raw))
	m, err = plugin.Load(dir)
	require.NoError(t, err)
	return m
}

func docKeys(doc string) map[string]bool {
	keys := map[string]bool{}
	for line := range strings.Lines(doc) {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "|") || len(cells) < 3 {
			continue
		}
		for _, k := range strings.FieldsFunc(cells[1], func(r rune) bool { return strings.ContainsRune(" ,/()", r) }) {
			keys[k] = true
		}
	}
	return keys
}

func keyLabel(combo string) string {
	parts := strings.Split(combo, "+")
	mods, key := parts[:len(parts)-1], parts[len(parts)-1]
	names := map[string]string{"return": "↩", "tab": "⇥", "space": "Space", "delete": "⌫", "esc": "⎋", "left": "←", "right": "→", "down": "↓", "up": "↑"}
	label, named := names[key]
	if !named {
		label = strings.ToUpper(key)
	}
	const plain, shifted = "1234567890=-][';\\,/.`", `!@#$%^&*()+_}{":|<?>~`
	if i := strings.Index(plain, key); !named && i >= 0 && slices.Contains(mods, "shift") {
		label = shifted[i : i+1]
	}
	var b strings.Builder
	for _, m := range []struct{ name, glyph string }{{"ctrl", "⌃"}, {"opt", "⌥"}, {"shift", "⇧"}, {"cmd", "⌘"}} {
		if slices.Contains(mods, m.name) {
			b.WriteString(m.glyph)
		}
	}
	return b.String() + label
}
