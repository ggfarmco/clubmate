package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
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

func TestSidebarHidesAgentNotesUntilToggled(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}}

	assert.Equal(t, []string{"dir:app/sdui", "app/sdui/nodes.md", "app/a.md"}, sidebarTree(t, d))

	d.AgentNotes = true
	assert.Equal(t, []string{"dir:app/sdui", "app/sdui/nodes.md", "app/a.md", "dir:app/.agents", "app/.agents/trap.md"}, sidebarTree(t, d))
	toggle, ok := findNode(sidebarView(d), agentNotesNode)
	require.True(t, ok)
	assert.Equal(t, true, toggle.Props["value"])
}

func TestSearchFiltersTree(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}, Query: "node", Hits: []hitRow{{Project: "app", Hit: Hit{Page: "sdui/nodes.md", Title: "Nodes", Snippet: "a node"}}}}

	assert.Equal(t, []string{"app/sdui/nodes.md"}, sidebarTree(t, d))
	_, recent := findNode(sidebarView(d), recentNode)
	assert.False(t, recent)
}

func TestMainShowsStaleBadgeFromField(t *testing.T) {
	page := Page{Path: "a.md", Title: "A", Updated: time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local), By: "Fix SDUI"}
	header := func(p Page) sdui.Node {
		h, ok := findNode(mainView(mainData{Project: "app", Page: &p, HTML: "<p>x</p>", Kind: sdui.DocumentMarkdown}), headerNode)
		require.True(t, ok)
		return h
	}

	assert.Nil(t, header(page).Props["badges"])
	assert.Equal(t, "updated Jan 2 15:04 by Fix SDUI", header(page).Props["meta"])
	page.Stale = true
	assert.Equal(t, []sdui.HeaderBadge{{Text: "stale", Tone: "warning"}}, header(page).Props["badges"])
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
	m := newWiki().pageData(t.Context(), contract.Workspace{}, nil, Open(p, time.Now), recentRow{Project: p.Name, Page: Page{Path: "<i>x</i>.md"}}, false)

	assert.NotContains(t, m.HTML, "<i>")
	assert.Contains(t, m.HTML, "&lt;i&gt;")
}
