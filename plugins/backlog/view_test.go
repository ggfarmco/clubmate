package main

import (
	"testing"
	"time"

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
	_, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "user"}, detail{hasTasks: true}), "status")
	assert.False(t, ok)
	status, ok := findNode(mainView(&Task{ID: 1, Title: "a", Status: statusTodo, Priority: priorityNormal}, detail{hasTasks: true}), "status")
	require.True(t, ok)
	for _, o := range status.Props["options"].([]sdui.Option) {
		assert.NotEqual(t, statusInProgress, o.Value)
	}
}

func TestMainEmptyState(t *testing.T) {
	assert.Equal(t, "Select a task", mainView(nil, detail{hasTasks: true}).Props["text"])
	empty := mainView(nil, detail{}).Children[0]
	require.Len(t, empty.Children, 2)
	assert.Equal(t, "Nothing in the backlog", empty.Children[0].Props["title"])
	assert.Equal(t, "new", empty.Children[1].ID)
	assert.Equal(t, "add", empty.Children[1].Props["action"])
}

func TestPrimaryFollowsOwner(t *testing.T) {
	primary := func(t *Task, owners map[string]owner) sdui.Node {
		header, _ := findNode(mainView(t, detail{owners: owners, hasTasks: true}), "heading")
		return header.Children[0]
	}
	todo := &Task{ID: 1, Title: "a", Status: statusTodo, Description: "x", Links: []Link{{Kind: linkBlocks, Task: 2}}, Attachments: []Attachment{{Kind: attachFile, Ref: "a.go"}, {Kind: attachURL, Ref: "https://x.y"}, {Kind: attachURL, Ref: "https://y.z"}}}
	start := primary(todo, nil)
	assert.Equal(t, "start", start.Props["action"])
	assert.Equal(t, "Start agent · description, 1 link, 3 attachments", start.Props["label"])
	assert.Equal(t, "cmd+return", start.Props["shortcut"])
	assert.Equal(t, "Start agent · 2 URLs", primary(&Task{ID: 1, Title: "a", Status: statusIdea, Attachments: todo.Attachments[1:]}, nil).Props["label"])

	inProgress := func(owner string) *Task {
		return &Task{ID: 1, Title: "a", Status: statusInProgress, ClaimedBy: owner}
	}
	run := map[string]owner{"agt_c": {label: "Review loop · Coder", session: true, run: "run_1", runTitle: "Review loop"}}
	assert.Equal(t, "Open Review loop", primary(inProgress("agt_c"), run).Props["label"])
	assert.Equal(t, "reveal", primary(inProgress("agt_c"), run).Props["action"])
	assert.Equal(t, "cmd+return", primary(inProgress("agt_c"), run).Props["shortcut"])
	closed := map[string]owner{"agt_c": {label: "x (closed)", session: true, stale: true}}
	assert.Equal(t, "start", primary(inProgress("agt_c"), closed).Props["action"])
	assert.Equal(t, "start", primary(inProgress(userOwner), nil).Props["action"])
	assert.Equal(t, "takeback", primary(inProgress("my-cli"), nil).Props["action"])
}

func TestLinksAndAttachmentsSections(t *testing.T) {
	task := &Task{ID: 1, Title: "a", Status: statusTodo, Links: []Link{{Kind: linkBlockedBy, Task: 8}}, Attachments: []Attachment{{Kind: attachFile, Ref: "design/check/fix-list.md"}}}
	main := mainView(task, detail{hasTasks: true, linked: map[int]Task{8: {ID: 8, Title: "Record missed runs", Status: statusDone}}, errs: map[string]string{linkField: "Bad link"}})
	links, ok := findNode(main, linksNode)
	require.True(t, ok)
	assert.Equal(t, []sdui.Item{{ID: "8", Label: "#8 Record missed runs", Detail: "blocked by · done", Icon: "checkmark.circle", Menu: []sdui.MenuEntry{{Action: actUnlink, Label: "Remove link"}}}}, links.Props["items"])
	field, ok := findNode(main, linkField)
	require.True(t, ok)
	assert.Equal(t, "Bad link", field.Props["error"])
	files, ok := findNode(main, attachNode)
	require.True(t, ok)
	assert.Equal(t, "design/check/fix-list.md", files.Props["items"].([]sdui.Item)[0].Label)
	_, ok = findNode(main, attachField)
	assert.True(t, ok)
	edit, _ := findNode(main, "edit")
	_, inForm := findNode(edit, linkField)
	assert.False(t, inForm)
}

func TestOwnerHistoryFallsBack(t *testing.T) {
	created := time.Date(2026, 11, 2, 14, 10, 0, 0, time.UTC)
	old := Task{CreatedAt: created, Status: statusInProgress, ClaimedBy: "agt_b", LastClaimedBy: "agt_a"}
	assert.Equal(t, []OwnerMove{{Owner: userOwner, At: created, Why: whyCreated}, {Owner: "agt_a", Why: whyClaimed}, {Owner: "agt_b", Why: whyClaimed}}, ownerHistory(old))
	released := Task{CreatedAt: created, Status: statusTodo, LastClaimedBy: "agt_a"}
	assert.Equal(t, []OwnerMove{{Owner: userOwner, At: created, Why: whyCreated}, {Owner: "agt_a", Why: whyClaimed}, {Owner: userOwner, Why: whyReleased}}, ownerHistory(released))
}

func TestParseLinkAndAttachment(t *testing.T) {
	for text, want := range map[string]Link{"blocks #3": {linkBlocks, 3}, "Blocked by 5": {linkBlockedBy, 5}, "#7": {linkRelates, 7}, "relates to #2": {linkRelates, 2}} {
		got, err := parseLink(text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
	}
	_, err := parseLink("blocks x")
	assert.ErrorIs(t, err, errLinkText)
	for text, kind := range map[string]string{"design/a.md": attachFile, "shots/run.png": attachImage, "https://claude.ai/artifact/abc": attachArtifact, "https://x.y/a": attachURL} {
		assert.Equal(t, kind, parseAttachment(text).Kind, text)
	}
}

func TestMainDeleteConfirms(t *testing.T) {
	del, ok := findNode(mainView(&Task{ID: 7, Title: "a", Status: statusDone, Priority: priorityNormal}, detail{hasTasks: true}), "delete")
	require.True(t, ok)
	assert.Equal(t, "Delete task #7?", del.Props["confirm"])
}

func TestMainFormAutosaves(t *testing.T) {
	main := mainView(&Task{ID: 7, Title: "a", Status: statusTodo, Priority: priorityNormal}, detail{hasTasks: true})
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
	owners := map[string]owner{"agt_b": {label: ownerLabel("agt_b", "Refactor", true), session: true, stale: true}}
	main := mainView(&Task{ID: 4, Title: "Old", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_b"}, detail{owners: owners, hasTasks: true})
	header, ok := findNode(main, "heading")
	require.True(t, ok)
	assert.Equal(t, "#4 · In progress by Refactor (closed)", header.Props["meta"])
}

func TestMainHeaderIsTheTitleField(t *testing.T) {
	main := mainView(&Task{ID: 7, Title: "Fix login", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_a"}, detail{hasTasks: true, errs: map[string]string{"title": "Title must not be empty"}})
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

	doc, ok := findNode(mainView(task, detail{hasTasks: true}), "doc")
	require.True(t, ok)
	assert.Equal(t, "document", doc.Type)
	assert.Equal(t, sdui.SizeFit, doc.Props["size"])
	assert.Contains(t, doc.Props["html"], "<table>")
	_, ok = findNode(mainView(task, detail{hasTasks: true}), "description")
	assert.False(t, ok)

	editing := mainView(task, detail{hasTasks: true, editing: true})
	_, ok = findNode(editing, "doc")
	assert.False(t, ok)
	_, ok = findNode(editing, "description")
	assert.True(t, ok)
	toggle, ok := findNode(editing, "describe")
	require.True(t, ok)
	assert.Equal(t, "Done editing", toggle.Props["label"])

	empty := mainView(&Task{ID: 3, Title: "b", Status: statusTodo, Priority: priorityNormal}, detail{hasTasks: true})
	_, ok = findNode(empty, "description")
	assert.True(t, ok)
	_, ok = findNode(empty, "describe")
	assert.False(t, ok)
}

func TestViewShortcutsDeclared(t *testing.T) {
	shortcuttest.RequireDeclared(t, mainView(&Task{ID: 1, Title: "a", Status: statusTodo, Priority: priorityNormal}, detail{hasTasks: true}))
}
