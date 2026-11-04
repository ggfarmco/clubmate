package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	isdui "clubmate/internal/sdui"
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
	empty := mainView(nil, detail{}).Children[0].Children[0]
	assert.Equal(t, "Nothing in the backlog", empty.Props["title"])
	assert.Equal(t, true, empty.Props["center"])
	require.Len(t, empty.Children, 2)
	assert.Equal(t, "new", empty.Children[0].ID)
	assert.Equal(t, "add", empty.Children[0].Props["action"])
	assert.Equal(t, "lifecycle", empty.Children[1].ID)
	require.NoError(t, isdui.Validate(mainView(nil, detail{})))
}

func TestImageOpensOnlyInsideTheRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.png"), nil, 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "b.png")))
	_, err := insideRoot(root, filepath.Join(root, "a.png"))
	assert.NoError(t, err)
	_, err = insideRoot(root, filepath.Join(root, "b.png"))
	assert.ErrorIs(t, err, errNotInRoot)
	_, err = insideRoot("", filepath.Join(root, "a.png"))
	assert.ErrorIs(t, err, errNotInRoot)
}

func TestAttachmentOpens(t *testing.T) {
	for _, tt := range []struct {
		a     Attachment
		opens string
		path  string
	}{
		{Attachment{Kind: attachURL, Ref: "https://x.y/a"}, opensURL, ""},
		{Attachment{Kind: attachArtifact, Ref: "https://claude.ai/artifact/1"}, opensURL, ""},
		{Attachment{Kind: attachFile, Ref: "design/check/fix-list.md"}, opensDoc, "/r/design/check/fix-list.md"},
		{Attachment{Kind: attachArtifact, Ref: "design/board.html"}, opensDoc, "/r/design/board.html"},
		{Attachment{Kind: attachImage, Ref: "shots/missed-run.png"}, opensSystem, "/r/shots/missed-run.png"},
		{Attachment{Kind: attachFile, Ref: "plugins/backlog/view.go"}, opensEditor, "/r/plugins/backlog/view.go"},
	} {
		v := attachmentOf(tt.a, "/r")
		assert.Equal(t, tt.opens, v.Opens, tt.a.Ref)
		assert.Equal(t, tt.path, v.Path, tt.a.Ref)
	}
}

func TestPrimaryFollowsOwner(t *testing.T) {
	primary := func(t *Task, owners map[string]owner) sdui.Node {
		header, _ := findNode(mainView(t, detail{owners: owners, hasTasks: true}), "heading")
		return header.Children[0]
	}
	todo := &Task{ID: 1, Title: "a", Status: statusTodo, Description: "x", Links: []Link{{Kind: linkBlocks, Task: 2}}, Attachments: []Attachment{{Kind: attachFile, Ref: "a.go"}, {Kind: attachURL, Ref: "https://x.y"}, {Kind: attachURL, Ref: "https://y.z"}}}
	start := primary(todo, nil)
	assert.Equal(t, "start", start.Props["action"])
	assert.Equal(t, "Start agent", start.Props["label"])
	assert.Equal(t, "cmd+return", start.Props["shortcut"])
	require.Len(t, start.Children, 1)
	assert.Equal(t, "start", start.Children[0].Props["action"])
	raw, err := json.Marshal(start.Children[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"text":"description, 1 link, 3 attachments"`)
	assert.Equal(t, "2 URLs", startSummary(&Task{Attachments: todo.Attachments[1:]}))

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

func TestOwnerRowsAndLanes(t *testing.T) {
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, time.Local) }
	task := &Task{ID: 4, Title: "Old", Status: statusInProgress, Priority: priorityNormal, ClaimedBy: "agt_c", LastClaimedBy: "agt_b", CreatedBy: userOwner, Owners: []OwnerMove{
		{Owner: userOwner, At: at(2, 14, 10), Why: whyCreated},
		{Owner: "agt_b", Title: "Fix", At: at(2, 16, 2), Why: whyStarted, Failure: "exit 1"},
		{Owner: userOwner, At: at(2, 17, 20), Why: whyStopped, Failure: "exit 1"},
		{Owner: "agt_c", Run: "run_1", RunTitle: "Review loop", At: at(3, 15, 3), Why: whyClaimed},
	}}
	owners := map[string]owner{
		"agt_b": {label: "Fix (closed)", session: true, stale: true},
		"agt_c": {label: "Review loop · Coder", session: true, run: "run_1", runTitle: "Review loop"},
	}
	main := mainView(task, detail{owners: owners, hasTasks: true})

	header, _ := findNode(main, "heading")
	assert.Equal(t, "#4", header.Props["meta"])
	chip, ok := findNode(main, "owner")
	require.True(t, ok)
	assert.Equal(t, "Review loop · Coder", chip.Props["label"])
	assert.Contains(t, mustText(t, mainView(task, detail{owners: map[string]owner{"agt_c": {label: "Coder", session: true, state: "running"}}, hasTasks: true})), "running")
	assert.Equal(t, actReveal, chip.Props["action"])
	last, ok := findNode(main, "lastOwner")
	require.True(t, ok)
	assert.Equal(t, actRevealLast, last.Props["action"])
	assert.Equal(t, failedIcon, last.Props["icon"])
	assert.Contains(t, mustText(t, main), `"text":"stopped · exit 1","tone":"danger"`)

	lanes, ok := findNode(main, ownersNode)
	require.True(t, ok)
	assert.Equal(t, "lanes", lanes.Type)
	assert.Equal(t, []sdui.LaneActor{
		{ID: "you", Label: "you", Icon: "person"},
		{ID: "agt_b", Label: "Fix (closed)", Icon: failedIcon, Tone: "danger"},
		{ID: "run:run_1", Label: "Review loop · run", Icon: graphIcon},
	}, lanes.Props["actors"])
	assert.Equal(t, []sdui.LaneMove{
		{Actor: "you", Cause: -1, Label: "created", Tooltip: "you · created · Nov 2 14:10", Time: "Nov 2 14:10"},
		{Actor: "agt_b", Cause: 0, Label: "Start agent", Tooltip: "Fix (closed) · Start agent · Nov 2 16:02", User: true, Tone: "danger", Ref: "agt_b", Time: "16:02"},
		{Actor: "you", Cause: 1, Label: "failed → back", Tooltip: "you · stopped · exit 1 · Nov 2 17:20", Time: "17:20"},
		{Actor: "run:run_1", Cause: 2, Label: "claimed", Tooltip: "Review loop · run · claimed · Nov 3 15:03", Ref: "run:run_1", Current: true, Last: true, Time: "Nov 3 15:03"},
	}, lanes.Props["moves"])
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

func mustText(t *testing.T, n sdui.Node) string {
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	return string(raw)
}

func TestOwnerLanesTellSameTitlesApart(t *testing.T) {
	first, second := time.Date(2026, 11, 3, 14, 5, 0, 0, time.Local), time.Date(2026, 11, 3, 14, 5, 30, 0, time.Local)
	task := &Task{ID: 1, Title: "T", Status: statusInProgress, ClaimedBy: "agt_b", Owners: []OwnerMove{
		{Owner: userOwner, Why: whyCreated},
		{Owner: "agt_a", Title: "T", At: first, Why: whyStarted},
		{Owner: "agt_b", Title: "T", At: second, Why: whyStarted},
	}}
	var labels []string
	owners := map[string]owner{"agt_a": {label: "T (closed)", session: true, stale: true}}
	for _, a := range ownerLanes(task, owners).Props["actors"].([]sdui.LaneActor) {
		labels = append(labels, a.Label)
	}
	assert.Equal(t, []string{"you", "14:05:00 · T (closed)", "14:05:30 · T"}, labels)
}

func TestDropRefsStayInsideTheWorktree(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "design"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "design", "board.png"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), nil, 0o644))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")))

	refs, err := dropRefs(root, []string{filepath.Join(root, "design", "board.png")})
	require.NoError(t, err)
	assert.Equal(t, []string{"design/board.png"}, refs)
	for _, bad := range []string{filepath.Join(outside, "secret"), filepath.Join(root, "link"), filepath.Join(root, "design")} {
		_, err := dropRefs(root, []string{bad})
		assert.ErrorIs(t, err, errNotInRoot, bad)
	}
}

func TestBranchFor(t *testing.T) {
	assert.Equal(t, "backlog/4-fix-the-flaky-switcher-test", branchFor(&Task{ID: 4, Title: "Fix the flaky switcher test!"}))
	assert.Equal(t, "backlog/5", branchFor(&Task{ID: 5, Title: "Ёлка"}))
}
