package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/shortcuttest"
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
	tree, ok := findNode(sidebarView(tasks, "", map[string]bool{statusDone: true}, 0, ""), "tasks")
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

func TestSidebarFiltersByTag(t *testing.T) {
	tasks := []Task{
		{ID: 1, Title: "Mic", Status: statusTodo, Priority: priorityNormal, Tags: []string{"audio", "phone"}},
		{ID: 2, Title: "Push", Status: statusTodo, Priority: priorityNormal, Tags: []string{"phone"}},
		{ID: 3, Title: "Plain", Status: statusIdea, Priority: priorityNormal},
		{ID: 4, Title: "Shipped", Status: statusDone, Priority: priorityNormal, Tags: []string{"phone", "old"}},
	}
	side := sidebarView(tasks, "", nil, 0, "audio")
	tabs, ok := findNode(side, "tags")
	require.True(t, ok)
	assert.Equal(t, []sdui.Item{{ID: "all", Label: "All", Badge: "4"}, {ID: "tag:phone", Label: "phone", Badge: "2"}, {ID: "tag:audio", Label: "audio", Badge: "1"}}, tabs.Props["items"])
	assert.Equal(t, "tag:audio", tabs.Props["selected"])
	tree, ok := findNode(side, "tasks")
	require.True(t, ok)
	groups := tree.Props["items"].([]sdui.TreeItem)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Children, 1)
	assert.Equal(t, "1", groups[0].Children[0].ID)

	_, ok = findNode(sidebarView(tasks[2:], "", nil, 0, ""), "tags")
	assert.False(t, ok)
}

func TestMainOmitsStatusWhileInProgress(t *testing.T) {
	_, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "user"}, nil, true, nil, false), "status")
	assert.False(t, ok)
	status, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusTodo, Priority: priorityNormal}, nil, true, nil, false), "status")
	require.True(t, ok)
	for _, o := range status.Props["options"].([]sdui.Option) {
		assert.NotEqual(t, statusInProgress, o.Value)
	}
}

func TestMainEmptyState(t *testing.T) {
	assert.Equal(t, "No tasks yet — add one on the left", mainView(nil, nil, false, nil, false).Props["text"])
	assert.Equal(t, "Select a task", mainView(nil, nil, true, nil, false).Props["text"])
}

func TestMainDeleteConfirms(t *testing.T) {
	del, ok := findNode(mainView(&Task{ID: 7, Title: "a", Status: statusDone, Priority: priorityNormal}, nil, true, nil, false), "delete")
	require.True(t, ok)
	assert.Equal(t, "Delete task #7?", del.Props["confirm"])
}

func TestMainFormAutosaves(t *testing.T) {
	main := mainView(&Task{ID: 7, Title: "a", Status: statusTodo, Priority: priorityNormal}, nil, true, nil, false)
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
	main := mainView(&Task{ID: 4, Title: "Old", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_b"}, owners, true, nil, false)
	header, ok := findNode(main, "heading")
	require.True(t, ok)
	assert.Equal(t, "#4 · In progress by Refactor (closed)", header.Props["meta"])
}

func TestMainHeaderIsTheTitleField(t *testing.T) {
	main := mainView(&Task{ID: 7, Title: "Fix login", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_a"}, nil, true, map[string]string{"title": "Title must not be empty"}, false)
	form, ok := findNode(main, "edit")
	require.True(t, ok)
	header, ok := findNode(form, "heading")
	require.True(t, ok)
	assert.Equal(t, "Fix login", header.Props["title"])
	assert.Equal(t, "title", header.Props["field"])
	assert.Equal(t, "Title must not be empty", header.Props["error"])
	require.NotEmpty(t, header.Children)
	for _, b := range header.Children {
		assert.NotEmpty(t, b.Props["action"], b.ID)
	}
}

func TestMainDescriptionIsADocumentUntilEdited(t *testing.T) {
	task := &Task{ID: 2, Title: "a", Status: statusTodo, Priority: priorityNormal, Description: "| a | b |\n|---|---|\n| 1 | 2 |"}

	doc, ok := findNode(mainView(task, nil, true, nil, false), "doc")
	require.True(t, ok)
	assert.Equal(t, "document", doc.Type)
	assert.Equal(t, sdui.SizeFit, doc.Props["size"])
	assert.Contains(t, doc.Props["html"], "<table>")
	_, ok = findNode(mainView(task, nil, true, nil, false), "description")
	assert.False(t, ok)

	editing := mainView(task, nil, true, nil, true)
	_, ok = findNode(editing, "doc")
	assert.False(t, ok)
	_, ok = findNode(editing, "description")
	assert.True(t, ok)
	toggle, ok := findNode(editing, "describe")
	require.True(t, ok)
	assert.Equal(t, "Done editing", toggle.Props["label"])

	empty := mainView(&Task{ID: 3, Title: "b", Status: statusTodo, Priority: priorityNormal}, nil, true, nil, false)
	_, ok = findNode(empty, "description")
	assert.True(t, ok)
	_, ok = findNode(empty, "describe")
	assert.False(t, ok)
}

func TestViewShortcutsDeclared(t *testing.T) {
	shortcuttest.RequireDeclared(t, mainView(&Task{ID: 1, Title: "a", Status: statusTodo, Priority: priorityNormal}, nil, true, nil, false))
}
