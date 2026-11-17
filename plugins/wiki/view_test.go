package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
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

func treeIDs(items []sdui.TreeItem) []string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
		ids = append(ids, treeIDs(it.Children)...)
	}
	return ids
}

func sidebarTree(t *testing.T, d sidebarData) []string {
	t.Helper()
	tree, ok := findNode(sidebarView(d), pagesNode)
	require.True(t, ok)
	return treeIDs(tree.Props["items"].([]sdui.TreeItem))
}

func demoProject() projectView {
	return projectView{Name: "app", Pages: []Page{
		{Path: ".agents/trap.md", Title: "Trap", Shadow: true},
		{Path: "a.md", Title: "A"},
		{Path: "sdui/nodes.md", Title: "Nodes"},
	}}
}

func TestSidebarFoldsAgentNotesLast(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}}

	assert.Equal(t, []string{"app/a.md", "dir:app/sdui", "app/sdui/nodes.md", "dir:app/.agents"}, sidebarTree(t, d))

	d.Selected = "app/.agents/trap.md"
	assert.Equal(t, []string{"app/a.md", "dir:app/sdui", "app/sdui/nodes.md", "dir:app/.agents", "app/.agents/trap.md"}, sidebarTree(t, d))
}

func TestOnlyTopLevelAgentsFolderIsAgentNotes(t *testing.T) {
	w := newWiki()
	fold := func(id, action string) {
		raw, err := json.Marshal(id)
		require.NoError(t, err)
		require.NoError(t, w.handle(t.Context(), pluginsdk.UIEvent{WorkspaceID: "ws", NodeID: pagesNode, Action: action, Value: raw}))
	}

	fold("dir:app/guides/.agents", "collapse")
	fold("dir:app/.agents", "expand")

	u := w.workspace("ws")
	assert.True(t, u.collapsed["dir:app/guides/.agents"])
	assert.True(t, u.agentNotes)
}

func TestSearchFiltersTree(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}, Query: "node", Hits: []hitRow{{Project: "app", Hit: Hit{Page: "sdui/nodes.md", Title: "Nodes", Snippet: "a node"}}}}

	assert.Equal(t, []string{"app/sdui/nodes.md"}, sidebarTree(t, d))
}

func TestMainShowsStaleBadgeFromField(t *testing.T) {
	page := Page{Path: "a.md", Title: "A", Updated: time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local), By: "Fix SDUI"}
	header := func(p Page) sdui.Node {
		h, ok := findNode(mainView(mainData{Project: "app", Page: &p, HTML: "<p>x</p>", Kind: sdui.DocumentMarkdown, Now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.Local)}), headerNode)
		require.True(t, ok)
		return h
	}

	assert.Nil(t, header(page).Props["badges"])
	assert.Equal(t, "updated Jan 2 15:04 by Fix SDUI", header(page).Props["meta"])
	page.Stale = true
	assert.Equal(t, []sdui.HeaderBadge{{Text: "stale", Tone: "warning"}}, header(page).Props["badges"])
}

func TestMetaShowsTimeOnlyForToday(t *testing.T) {
	at := time.Date(2026, 11, 16, 18, 20, 0, 0, time.Local)

	assert.Equal(t, "updated 18:20 by you", meta(at, "you", at.Add(3*time.Hour)))
	assert.Equal(t, "updated Nov 16 18:20 by you", meta(at, "you", at.Add(8*time.Hour)))
	assert.Equal(t, "updated Nov 16, 2026 18:20 by you", meta(at, "you", at.AddDate(1, 0, 0)))
}

func TestDropTitleOnlyWhenItMatches(t *testing.T) {
	assert.Equal(t, "<a id=\"a\"></a><p>x</p>", dropTitle("<h1 id=\"a\">A &amp; <code>B</code></h1>\n<p>x</p>", "A & B"))
	assert.Equal(t, "<h1>Other</h1><p>x</p>", dropTitle("<h1>Other</h1><p>x</p>", "A"))
	assert.Equal(t, "<p>x</p><h1>A</h1>", dropTitle("<p>x</p><h1>A</h1>", "A"))
}

func TestEmptyWikiShowsNewPage(t *testing.T) {
	n := mainView(mainData{})

	assert.Equal(t, "empty", n.Type)
	_, ok := findNode(n, newNode)
	assert.True(t, ok)
}

func TestRenderKeepsSelectionMadeMeanwhile(t *testing.T) {
	w := newWiki()
	u := w.workspace("ws")
	u.selected = "app/b.md"

	w.settleSelection(u, "app/a.md", "app/a.md")
	assert.Equal(t, "app/b.md", u.selected)

	w.settleSelection(u, "app/b.md", "app/c.md")
	assert.Equal(t, "app/c.md", u.selected)
}

func TestReadErrorIsEscaped(t *testing.T) {
	p, _ := newProject(t)
	m := newWiki().pageData(t.Context(), contract.Workspace{}, nil, Open(p, time.Now), recentRow{Project: p.Name, Page: Page{Path: "<i>x</i>.md"}}, nil, false)

	assert.NotContains(t, m.HTML, "<i>")
	assert.Contains(t, m.HTML, "&lt;i&gt;")
}
