package main_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type pickerAddRepos struct {
	Picker struct {
		AddRepos []struct {
			Folder string `json:"folder"`
			Title  string `json:"title"`
		} `json:"addRepos"`
	} `json:"picker"`
}

func reposParent(t *testing.T) (string, string, string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	older := repoIn(t, parent, "older")
	t.Setenv("GIT_COMMITTER_DATE", "2030-01-01T10:00:00Z")
	newer := repoIn(t, parent, "newer")
	return parent, older, newer
}

func (f *fixture) waitReposSheet(t *testing.T, workspace string) sdui.Node {
	t.Helper()
	return f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		sheets := sheetNodes(n, "sheet")
		return len(sheets) == 1 && sheets[0].ID == "reposSheet" && len(sheetNodes(n, "toggle")) > 1
	})
}

func (f *fixture) addRepos(t *testing.T, workspace string) pickerAddRepos {
	t.Helper()
	var out pickerAddRepos
	require.NoError(t, f.run(t, workspace, "agents.worktrees", map[string]any{}, &out))
	return out
}

func (f *fixture) reposSheetOpen(t *testing.T, workspace string) bool {
	t.Helper()
	var views []struct {
		Slot string     `json:"slot"`
		Tree *sdui.Node `json:"tree"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": workspace}, &views))
	for _, v := range views {
		if v.Slot == "agents.sheet" && v.Tree != nil {
			sheets := sheetNodes(*v.Tree, "sheet")
			return len(sheets) == 1 && sheets[0].ID == "reposSheet"
		}
	}
	return false
}

func TestReposSheetIsOfferedOnceAndRemembered(t *testing.T) {
	f := startGit(t, map[string]any{})
	parent, _, _ := reposParent(t)
	workspace := f.workspace(t, parent)

	sheet := f.waitReposSheet(t, workspace)
	assert.Equal(t, "Add repos from "+filepath.Base(parent), sheetNodes(sheet, "sheet")[0].Props["title"])
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/cancel", "addReposCancel", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })

	f.restartAgents(t, workspace)
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.unsubscribe", map[string]string{"subscription": f.subs[workspace]}, nil))
	f.subscribe(t, workspace)

	testutil.Never(t, func() bool { return f.reposSheetOpen(t, workspace) }, 2*time.Second)
}

func TestAddedReposComeAfterTheParentInSheetOrder(t *testing.T) {
	f := startGit(t, map[string]any{})
	parent, older, newer := reposParent(t)
	workspace := f.workspace(t, parent)
	f.waitReposSheet(t, workspace)
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/cancel", "addReposCancel", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	offers := f.addRepos(t, workspace).Picker.AddRepos
	require.Len(t, offers, 1)
	assert.Equal(t, parent, offers[0].Folder)
	assert.Equal(t, "Add repos from "+filepath.Base(parent)+"…", offers[0].Title)

	require.NoError(t, f.run(t, workspace, "agents.repos-sheet", map[string]any{"path": parent}, nil))
	sheet := f.waitReposSheet(t, workspace)
	var boxes []string
	for _, n := range sheetNodes(sheet, "toggle") {
		boxes = append(boxes, n.ID)
	}
	assert.Equal(t, []string{"reposSheet/all", "reposSheet/tick/" + newer, "reposSheet/tick/" + older}, boxes)
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/all", "addReposAll", true)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		buttons := sheetNodes(n, "button")
		return len(buttons) == 2 && buttons[1].Props["label"] == "Add 2 to demo" && buttons[1].Props["disabled"] == nil
	})
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/add", "addReposConfirm", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.Equal(t, []string{parent, newer, older}, f.folderPaths(t, workspace))
	for _, folder := range f.folders(t, workspace)[1:] {
		assert.Empty(t, folder.AddedBy)
		assert.True(t, folder.Seen)
	}
	assert.Empty(t, f.addRepos(t, workspace).Picker.AddRepos)
}

func TestAnAddedRepoIsADisabledTickedBox(t *testing.T) {
	f := startGit(t, map[string]any{})
	parent, older, newer := reposParent(t)
	workspace := f.workspace(t, parent)
	f.waitReposSheet(t, workspace)
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/tick/"+newer, "addReposTick", true)
	f.h.Event(t, workspace, "agents.sheet", "reposSheet/add", "addReposConfirm", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })

	require.NoError(t, f.run(t, workspace, "agents.repos-sheet", map[string]any{"path": parent}, nil))
	boxes := map[string]sdui.Node{}
	for _, n := range sheetNodes(f.waitReposSheet(t, workspace), "toggle") {
		boxes[n.ID] = n
	}
	assert.Equal(t, map[string]any{"value": true, "label": "newer", "style": "checkbox", "disabled": true}, boxes["reposSheet/tick/"+newer].Props)
	assert.Nil(t, boxes["reposSheet/tick/"+older].Props["disabled"])
	assert.Nil(t, boxes["reposSheet/all"].Props["disabled"])

	f.h.Event(t, workspace, "agents.sheet", "reposSheet/filter", "addReposFilter", "newer")

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		toggles := sheetNodes(n, "toggle")
		return len(toggles) == 2 && toggles[0].Props["disabled"] == true && toggles[0].Props["value"] == true
	})
}

func TestAWorkspaceOfReposIsOfferedNothing(t *testing.T) {
	f := startGit(t, map[string]any{})
	workspace := f.workspace(t, newRepo(t))

	assert.Empty(t, f.addRepos(t, workspace).Picker.AddRepos)
	testutil.Never(t, func() bool { return f.reposSheetOpen(t, workspace) }, time.Second)
}
