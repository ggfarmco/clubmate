package main_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type editorSettings struct {
	Editor         string `json:"editor"`
	VSCodeAccepted bool   `json:"vscodeAccepted"`
}

func TestSettingsDefaultNvim(t *testing.T) {
	h := startSettings(t)
	assert.Equal(t, editorSettings{Editor: "nvim"}, settingsOf(t, h))
}

func TestUnknownEditorRejected(t *testing.T) {
	h := startSettings(t)
	require.ErrorContains(t, setEditor(t, h, "emacs"), "emacs")
	assert.Equal(t, editorSettings{Editor: "nvim"}, settingsOf(t, h))
}

func TestSetEditorVSCodeRefusedWhileUnsaved(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, e.open(t, e.file(t, "a.go"), 1))
	require.NoError(t, dial(t, e.sock).Command("normal! ix"))
	require.ErrorContains(t, setEditor(t, e.h, "vscode"), "Save or discard unsaved changes first: a.go")
	assert.Equal(t, "nvim", settingsOf(t, e.h).Editor)
	assert.Equal(t, []string{e.sessionID}, e.running(t))
}

func TestSetEditorPublishesAndStopsNvims(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, setEditor(t, e.h, "vscode"))
	assert.Equal(t, "vscode", settingsOf(t, e.h).Editor)
	events, err := e.h.Bus.Read(t.Context(), "editor-nvim.settings-changed", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	var published editorSettings
	require.NoError(t, json.Unmarshal(events[0].Payload, &published))
	assert.Equal(t, "vscode", published.Editor)
	testutil.Eventually(t, func() bool { return len(e.running(t)) == 0 }, restartWait)
}

func startSettings(t *testing.T) *testutil.PluginHarness {
	t.Helper()
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"))
	h.WaitSlot(t, "", "editor-nvim.settings", func(n sdui.Node) bool { return contains(n, "select") })
	return h
}

func settingsOf(t *testing.T, h *testutil.PluginHarness) editorSettings {
	t.Helper()
	var s editorSettings
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.settings", "workspaceId": ""}, &s))
	return s
}

func setEditor(t *testing.T, h *testutil.PluginHarness, editor string) error {
	t.Helper()
	return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.settings-set", "workspaceId": "", "args": map[string]string{"editor": editor}}, nil)
}
