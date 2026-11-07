package main_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func (f *fixture) slotTree(t *testing.T, workspace, slot string) string {
	t.Helper()
	var views []struct {
		Slot string          `json:"slot"`
		Tree json.RawMessage `json:"tree"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": workspace}, &views))
	for _, v := range views {
		if v.Slot == slot {
			return string(v.Tree)
		}
	}
	return ""
}

func (f *fixture) sidebarItem(t *testing.T, workspace, id string) sdui.TreeItem {
	t.Helper()
	var root struct {
		Children []struct {
			Type  string `json:"type"`
			Props struct {
				Items []sdui.TreeItem `json:"items"`
			} `json:"props"`
		} `json:"children"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.slotTree(t, workspace, "agents.sidebar")), &root))
	for _, child := range root.Children {
		for _, item := range child.Props.Items {
			if item.ID == id {
				return item
			}
		}
	}
	require.Fail(t, "no such sidebar row", id)
	return sdui.TreeItem{}
}

func (f *fixture) fire(t *testing.T, workspace, slot, node, action string, value any) {
	t.Helper()
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": workspace, "slot": slot, "nodeId": node, "action": action, "value": value}, nil))
}

func (f *fixture) pick(t *testing.T, workspace, session string) {
	t.Helper()
	f.fire(t, workspace, "agents.sidebar", "sessions", "select", session)
}

func TestSiblingKeysWalkTheTaskInLandingOrder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 2, 1)
	f.subscribe(t, l.workspace)

	f.pick(t, l.workspace, l.kids[1].Session)
	testutil.Eventually(t, func() bool { return containsText(f.slotTree(t, l.workspace, "agents.main"), "1 of 2") }, wait)

	f.fire(t, l.workspace, "agents.main", l.kids[1].Session+"/sibling/next", "sibling", nil)
	testutil.Eventually(t, func() bool { return containsText(f.slotTree(t, l.workspace, "agents.main"), "2 of 2") }, wait)

	f.pick(t, l.workspace, l.owner)
	testutil.Eventually(t, func() bool { return !containsText(f.slotTree(t, l.workspace, "agents.main"), `"crumb"`) }, wait)
}

func TestFinishedTaskFoldsOnceOpened(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	f.subscribe(t, l.workspace)
	f.pick(t, l.workspace, l.kids[0].Session)
	assert.True(t, f.sidebarItem(t, l.workspace, l.owner).Expanded)

	l.land(t, 0)
	l.mergeInto(t, 0)
	require.Equal(t, "merged", l.report(t, 0, "merged").State)
	f.waitStatus(t, l.workspace, l.kids[0].Session, "done")

	testutil.Eventually(t, func() bool {
		f.pick(t, l.workspace, l.owner)
		return !f.sidebarItem(t, l.workspace, l.owner).Expanded
	}, wait)

	f.fire(t, l.workspace, "agents.sidebar", "sessions", "expand", l.owner)
	f.pick(t, l.workspace, l.kids[0].Session)
	f.pick(t, l.workspace, l.owner)
	assert.True(t, f.sidebarItem(t, l.workspace, l.owner).Expanded)
}

func TestRepoRowSelectsItsOrchestrator(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	f.connect(t, workspace, session, map[string]any{"path": other})
	f.subscribe(t, workspace)

	testutil.Eventually(t, func() bool { return len(f.sidebarItem(t, workspace, session).Children) == 1 }, wait)
	row := f.sidebarItem(t, workspace, session).Children[0]
	assert.Equal(t, "connect?", row.Detail)
	f.pick(t, workspace, row.ID)
	assert.Contains(t, f.slotTree(t, workspace, "agents.sidebar"), `"selected":"`+session+`"`)
}

func TestAFKRowRemovesTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": session, "on": true}, nil))
	require.Equal(t, "connected", f.connect(t, workspace, session, map[string]any{"path": other}).Status)
	f.subscribe(t, workspace)
	f.pick(t, workspace, session)
	require.Contains(t, f.slotTree(t, workspace, "agents.main"), "Remove from")

	f.fire(t, workspace, "agents.main", session+"/folderRemove/"+other, "folderRemove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
	assert.Len(t, f.folders(t, workspace), 2)
	f.fire(t, workspace, "agents.sheet", "folderSheet/remove", "folderConfirm", nil)
	testutil.Eventually(t, func() bool { return len(f.folders(t, workspace)) == 1 }, wait)
	testutil.Eventually(t, func() bool { return !containsText(f.slotTree(t, workspace, "agents.main"), "Remove from") }, wait)
}

func TestAFKRowInTheCodePanelKnowsTheFolder(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"), testutil.EchoPluginDirAs(t, codeManifest))
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": session, "on": true}, nil))
	require.Equal(t, "connected", f.connect(t, workspace, session, map[string]any{"path": other}).Status)
	f.subscribe(t, workspace)
	f.pick(t, workspace, f.newSession(t, workspace))
	f.fire(t, workspace, "editor-nvim.sidebar", "code/session", "codeSelect", session)

	f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", func(n sdui.Node) bool {
		return strings.Contains(string(rawTree(t, n)), "Remove from")
	})
}

func containsText(tree, text string) bool {
	return strings.Contains(tree, text)
}
