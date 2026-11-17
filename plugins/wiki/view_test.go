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

func TestFolderIndexPageIsTheFirstRowOrTheFolderRow(t *testing.T) {
	d := sidebarData{AgentNotes: true, Selected: "app/guides/sdui/index.md", Projects: []projectView{{Name: "app", Pages: []Page{
		{Path: ".agents/index.md", Title: "Notes index", Shadow: true},
		{Path: "guides/index.md", Title: "Guides home"},
		{Path: "guides/sdui/README.md", Title: "Readme"},
		{Path: "guides/sdui/index.md", Title: "SDUI protocol", Stale: true},
		{Path: "guides/sdui/nodes.md", Title: "Nodes"},
		{Path: "index.md", Title: "Home"},
		{Path: "solo/README.md", Title: "Solo"},
	}}}}

	tree, ok := findNode(sidebarView(d), pagesNode)
	require.True(t, ok)
	items := tree.Props["items"].([]sdui.TreeItem)
	assert.Equal(t, []string{"app/index.md", "app/solo/README.md", "dir:app/guides", "app/guides/index.md", "dir:app/guides/sdui", "app/guides/sdui/README.md", "app/guides/sdui/nodes.md", "dir:app/.agents", "app/.agents/index.md"}, treeIDs(items))
	assert.Equal(t, sdui.TreeItem{ID: "app/solo/README.md", Label: "Solo", Icon: folderPageIcon, Truncate: "tail"}, items[1])
	guides := items[2]
	assert.Equal(t, "guides", guides.Label)
	assert.Equal(t, sdui.TreeItem{ID: "app/guides/index.md", Label: "Guides home", Icon: folderPageIcon, Truncate: "tail"}, guides.Children[0])
	sub := guides.Children[1]
	assert.Equal(t, sdui.TreeItem{ID: "dir:app/guides/sdui", Label: "SDUI protocol", Icon: folderPageIcon, MirrorOf: "app/guides/sdui/index.md", Truncate: "tail", Badges: []sdui.HeaderBadge{{Text: staleBadge, Tone: "warning"}}, HasChildren: true, Expanded: true, Children: sub.Children}, sub)
	assert.Equal(t, d.Selected, tree.Props["selected"])
	assert.Equal(t, notesLabel, items[3].Label)

	d.Collapsed = map[string]bool{"dir:app/guides": true}
	tree, ok = findNode(sidebarView(d), pagesNode)
	require.True(t, ok)
	assert.Equal(t, "4", tree.Props["items"].([]sdui.TreeItem)[2].Detail)

	multi := folderItems("app", "", d.Projects[0].Pages, nil, false, false)
	assert.Equal(t, "app/guides/index.md", multi[2].MirrorOf)
}

func TestIndexPageHeaderUsesTheFolderPageIcon(t *testing.T) {
	h, ok := findNode(mainView(mainData{Page: &Page{Path: "guides/index.md", Title: "Guides"}, Kind: sdui.DocumentMarkdown, Index: true}), headerNode)
	require.True(t, ok)
	assert.Equal(t, folderPageIcon, h.Props["icon"])
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

func TestSearchShowsPagesThenAgentNotesWithSnippets(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}, Query: "node", Hits: []hitRow{
		{Project: "app", Hit: Hit{Page: ".agents/trap.md", Title: "Trap", Snippet: "a node trap"}},
		{Project: "app", Hit: Hit{Page: "sdui/nodes.md", Title: "Nodes", Snippet: "the node tree"}},
	}}

	tree, ok := findNode(sidebarView(d), pagesNode)
	require.True(t, ok)
	items := tree.Props["items"].([]sdui.TreeItem)
	assert.Equal(t, []string{hitsSection, "app/sdui/nodes.md", notesHits, "app/.agents/trap.md"}, treeIDs(items))
	assert.Equal(t, "…node tree", items[0].Children[0].Detail)
	assert.Equal(t, "…node trap", items[1].Children[0].Detail)
}

func TestNewToYouListsOthersChangesSinceOpened(t *testing.T) {
	since := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	row := func(page, by string, minutes int, shadow bool) recentRow {
		return recentRow{Project: "app", Page: Page{Path: page, Title: page, By: by, Updated: since.Add(time.Duration(minutes) * time.Minute), Shadow: shadow}}
	}
	all := []recentRow{
		row("h.md", "Claude", 80, false),
		row("g.md", "Claude", 70, false),
		row("f.md", "Claude", 60, false),
		row("e.md", "Claude", 50, false),
		row("d.md", "Claude", 40, false),
		row(".agents/n.md", "Claude", 35, true),
		row("mine.md", "you", 30, false),
		row("read.md", "Claude", 20, false),
		row("c.md", "Claude", 10, false),
		row("old.md", "Claude", -10, false),
	}
	st := readState{Since: since, Opened: map[string]time.Time{"app/read.md": since.Add(20 * time.Minute), "app/h.md": since.Add(time.Minute)}}

	paths := func(rows []recentRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Page.Path)
		}
		return out
	}

	assert.Equal(t, []string{"h.md", "g.md", "f.md", "e.md", "d.md"}, paths(newToYou(all, st)))
	assert.Equal(t, []string{"c.md"}, paths(newToYou(all[6:], st)))
}

func TestNewToYouSitsAboveLabelledRootPages(t *testing.T) {
	d := sidebarData{Projects: []projectView{demoProject()}, New: []recentRow{{Project: "app", Page: Page{Path: "sdui/nodes.md", Title: "Nodes", By: "Claude"}}}}

	assert.Equal(t, []string{newSection, "new:app/sdui/nodes.md", rootSection, "app/a.md", "dir:app/sdui", "app/sdui/nodes.md", "dir:app/.agents"}, sidebarTree(t, d))
}

func TestMarkMatchesOnlyText(t *testing.T) {
	body := `<h2 id="node">Node &amp; tree</h2><p><a href="node.md">a NODE</a></p><pre class="mermaid">node --> b</pre><script>node()</script><p>node</p>`

	got, ok := markMatches(body, "node", "m1")

	require.True(t, ok)
	assert.Equal(t, `<h2 id="node"><mark id="m1">Node</mark> &amp; tree</h2><p><a href="node.md">a <mark>NODE</mark></a></p><pre class="mermaid">node --> b</pre><script>node()</script><p><mark>node</mark></p>`, got)
	_, ok = markMatches(body, "absent", "m1")
	assert.False(t, ok)
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
	m := newWiki().pageData(t.Context(), contract.Workspace{}, nil, Open(p, time.Now), recentRow{Project: p.Name, Page: Page{Path: "<i>x</i>.md"}}, nil, false, find{})

	assert.NotContains(t, m.HTML, "<i>")
	assert.Contains(t, m.HTML, "&lt;i&gt;")
}

func TestDiffHTMLEscapesAndNumbersLines(t *testing.T) {
	diff := "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n@@ -2,3 +2,3 @@\n \n ## Setup\n-<b>old</b>\n+new & better\n\\ No newline at end of file\n"

	got := diffHTML(Change{Summary: "<i>fix</i>", By: "Agent", Added: 1, Removed: 1}, "Nov 16 10:05", diff, []string{"## Intro", "", "## Setup", "new & better"})

	assert.Contains(t, got, `<p class="diff-title">&lt;i&gt;fix&lt;/i&gt;</p>`)
	assert.Contains(t, got, `<p class="diff-at">Setup</p>`)
	assert.Contains(t, got, `<span class="line del"><span class="ln">4</span><span class="sign">−</span>&lt;b&gt;old&lt;/b&gt;`)
	assert.Contains(t, got, `<span class="line add"><span class="ln">4</span><span class="sign">+</span>new &amp; better`)
	assert.NotContains(t, got, "No newline")
}

func TestHistoryGroupsChangesByDay(t *testing.T) {
	day1 := time.Date(2026, 11, 16, 18, 20, 0, 0, time.Local)
	day0 := time.Date(2026, 11, 12, 9, 41, 0, 0, time.Local)
	d := mainData{Page: &Page{Path: "a.md", Title: "A"}, ShowHistory: true, Change: "r2", History: []Change{
		{Rev: "r3", At: day1, By: "Claude", Summary: "third"},
		{Rev: "r2", At: day1.Add(-time.Hour), By: "Claude", Summary: "second"},
		{Rev: "r1", At: day0, By: "you", Summary: "first"},
	}}

	tree, ok := findNode(mainView(d), changesNode)
	require.True(t, ok)
	items := tree.Props["items"].([]sdui.TreeItem)
	assert.Equal(t, []string{"day:2026-11-16", "r3", "r2", "day:2026-11-12", "r1"}, treeIDs(items))
	assert.Equal(t, "Claude · 18:20 · current", items[0].Children[0].Detail)
	assert.Equal(t, "r2", tree.Props["selected"])
	restore, ok := findNode(mainView(d), restoreNode)
	require.True(t, ok)
	assert.Equal(t, true, restore.Props["disabled"])
}
