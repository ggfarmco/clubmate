package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
)

func TestSelfSettingsReachSessions(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	dir := filepath.Join(f.h.Dirs.PluginState("agents"), "self")
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "prompt.md"))
		return err == nil
	}, 5*time.Second)
	write(t, filepath.Join(dir, "prompt.md"), "be brief")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "tidy"), 0o700))
	write(t, filepath.Join(dir, "skills", "tidy", "SKILL.md"), "tidy")

	f.prompt(t, workspace, f.newSession(t, workspace), "off")
	off := f.waitCall(t, "session/new", func(fakeCall) bool { return true })
	assert.NotContains(t, selfAppend(t, off), "be brief")
	assert.NotContains(t, string(off.Params), `"plugins"`)

	f.selfEvent(t, "self-enabled", true)
	testutil.Eventually(t, func() bool { return strings.Contains(f.settingsTree(t), "self-reset") }, 5*time.Second)
	assert.Contains(t, f.settingsTree(t), `"variant":"danger"`)
	f.prompt(t, workspace, f.newSession(t, workspace), "on")
	on := f.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(selfAppend(t, c), "be brief") })
	assert.Contains(t, string(on.Params), `"plugins":[{"type":"local","path":`)

	f.selfEvent(t, "self-reset", nil)
	assert.NotContains(t, f.settingsTree(t), "self-reset")
	f.prompt(t, workspace, f.newSession(t, workspace), "reset")
	testutil.Eventually(t, func() bool { return len(f.calls(t, "session/new")) == 3 }, wait)
	assert.NotContains(t, selfAppend(t, f.calls(t, "session/new")[2]), "be brief")
}

func TestDeclareRefusesOrdinarySession(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	err := f.runAs(t, workspace, session, "agents.self-declare", map[string]any{"summary": "x"}, nil)
	assert.ErrorContains(t, err, "only a night session may declare changes")
}

func TestDeclareCommitsInTheCallersSandbox(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, nightEcho)
	workspace := f.workspace(t, newRepo(t))
	box := filepath.Join(t.TempDir(), "box")
	git(t, filepath.Dir(box), "init", "-q", "--separate-git-dir="+box+".git", box)
	require.NoError(t, os.Remove(filepath.Join(box, ".git")))
	night := f.nightSession(t, workspace, box)
	f.waitStatus(t, workspace, night, "done")
	write(t, filepath.Join(box, "prompt.md"), "be brief")

	var out struct {
		Change string `json:"change"`
	}
	require.NoError(t, f.runAs(t, workspace, night, "agents.self-declare", map[string]any{"summary": "shorter"}, &out))
	head, err := exec.Command("git", "--git-dir="+box+".git", "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(string(head)), out.Change)
}

func TestUndoFromAnySession(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	dir := selfRepo(t, f)
	write(t, filepath.Join(dir, "prompt.md"), "be brief")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "shorter", "-m", "Clubmate-Self-Kind: edit\nClubmate-Self-Workspace: "+workspace)
	session := f.newSession(t, workspace)

	var list struct {
		Changes []struct {
			Change  string `json:"change"`
			Summary string `json:"summary"`
			Undone  bool   `json:"undone"`
		} `json:"changes"`
	}
	require.NoError(t, f.runAs(t, workspace, session, "agents.self-changes", nil, &list))
	require.Len(t, list.Changes, 1)
	assert.Equal(t, "shorter", list.Changes[0].Summary)

	require.NoError(t, f.runAs(t, workspace, session, "agents.self-undo", map[string]any{"change": list.Changes[0].Change}, nil))
	require.NoError(t, f.run(t, workspace, "agents.self-changes", nil, &list))
	assert.True(t, list.Changes[0].Undone)
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
	require.NoError(t, err)
	assert.Empty(t, prompt)
}

func TestNightCannotUndo(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, nightEcho)
	workspace := f.workspace(t, newRepo(t))
	selfRepo(t, f)
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")
	err := f.runAs(t, workspace, night, "agents.self-undo", map[string]any{"change": strings.Repeat("1", 40)}, nil)
	assert.ErrorContains(t, err, "a night session can't undo a change")
}

func selfRepo(t *testing.T, f *fixture) string {
	t.Helper()
	dir := filepath.Join(f.h.Dirs.PluginState("agents"), "self")
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, ".git", "refs", "heads", "main"))
		return err == nil
	}, 5*time.Second)
	return dir
}

func (f *fixture) selfEvent(t *testing.T, action string, value any) {
	t.Helper()
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": "", "slot": "agents.settings", "nodeId": action, "action": action, "value": value}, nil))
}

func (f *fixture) settingsTree(t *testing.T) string {
	t.Helper()
	var views []internalsdui.SlotView
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": ""}, &views))
	i := slices.IndexFunc(views, func(v internalsdui.SlotView) bool { return v.Slot == "agents.settings" })
	if i < 0 {
		return ""
	}
	return string(views[i].Tree)
}

func selfAppend(t *testing.T, c fakeCall) string {
	t.Helper()
	var params struct {
		Meta struct {
			SystemPrompt struct {
				Append string `json:"append"`
			} `json:"systemPrompt"`
		} `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(c.Params, &params))
	return params.Meta.SystemPrompt.Append
}
