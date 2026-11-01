package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func findNode(n sdui.Node, id string) (sdui.Node, bool) {
	if n.ID == id {
		return n, true
	}
	for _, c := range n.Children {
		if found, ok := findNode(c, id); ok {
			return found, true
		}
	}
	return sdui.Node{}, false
}

func TestSidebarGroupsByStatus(t *testing.T) {
	tasks := []Task{
		{ID: 3, Title: "Fix x", Status: statusInProgress, Priority: priorityHigh, ClaimedBy: "agt_a"},
		{ID: 4, Title: "Old", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_b"},
		{ID: 1, Title: "Plan", Status: statusTodo, Priority: priorityNormal},
		{ID: 2, Title: "Shipped", Status: statusDone, Priority: priorityNormal},
	}
	tree, ok := findNode(sidebarView(tasks, "", map[string]bool{statusDone: true}, 0), "tasks")
	require.True(t, ok)
	groups := tree.Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 3)
	assert.Equal(t, "group:in_progress", groups[0].ID)
	rows := groups[0].Children
	require.Len(t, rows, 2)
	assert.Equal(t, "3", rows[0].ID)
	assert.Equal(t, "Fix x", rows[0].Label)
	assert.Equal(t, "#3", rows[0].Detail)
	assert.Equal(t, "exclamationmark.2", rows[0].Icon)
	assert.Empty(t, rows[1].Icon)
	assert.Equal(t, "group:done", groups[2].ID)
	assert.False(t, groups[2].Expanded)
	assert.Empty(t, groups[2].Children)
}

func TestMainOmitsStatusWhileInProgress(t *testing.T) {
	_, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "user"}, nil, true, ""), "status")
	assert.False(t, ok)
	status, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusTodo, Priority: priorityNormal}, nil, true, ""), "status")
	require.True(t, ok)
	for _, o := range status.Props["options"].([]sdui.Option) {
		assert.NotEqual(t, statusInProgress, o.Value)
	}
}

func TestMainEmptyState(t *testing.T) {
	assert.Equal(t, "No tasks yet — add one on the left", mainView(nil, nil, false, "").Props["text"])
	assert.Equal(t, "Select a task", mainView(nil, nil, true, "").Props["text"])
}

func TestMainDeleteConfirms(t *testing.T) {
	del, ok := findNode(mainView(&Task{ID: 7, Title: "a", Status: statusDone, Priority: priorityNormal}, nil, true, ""), "delete")
	require.True(t, ok)
	assert.Equal(t, "Delete task #7?", del.Props["confirm"])
}

func TestMainFormAutosaves(t *testing.T) {
	main := mainView(&Task{ID: 7, Title: "a", Status: statusTodo, Priority: priorityNormal}, nil, true, "")
	form, ok := findNode(main, "edit")
	require.True(t, ok)
	assert.Equal(t, true, form.Props["autosave"])
	assert.Equal(t, "save.7", form.Props["action"])
	saved, ok := findNode(form, "save")
	require.True(t, ok)
	assert.Equal(t, "Saved", saved.Props["label"])
	assert.Equal(t, sdui.VariantGhost, saved.Props["variant"])
	assert.Equal(t, "cmd+s", saved.Props["shortcut"])
}

func TestMainNamesClosedClaimer(t *testing.T) {
	owners := map[string]string{"agt_b": ownerLabel("agt_b", "Refactor", true)}
	main := mainView(&Task{ID: 4, Title: "Old", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_b"}, owners, true, "")
	raw, err := json.Marshal(main)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"In progress by Refactor (closed)"`)
}
