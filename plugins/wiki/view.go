package main

import (
	"html"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	sidebarSlot   = "wiki.sidebar"
	mainSlot      = "wiki.main"
	searchNode    = "search"
	pagesNode     = "pages"
	headerNode    = "header"
	docNode       = "page"
	changesNode   = "changes"
	newNode       = "new"
	editNode      = "edit"
	historyNode   = "history"
	deleteNode    = "delete"
	dirPrefix     = "dir:"
	projectPrefix = "project:"
	staleBadge    = "stale"
	timeLayout    = "Jan 2 15:04"
	notesLabel    = "Agent notes"
	copyNode      = "copyPath"
	revealNode    = "reveal"
	emptyText     = "Agents write here as they work: how things work, why they were decided, examples. You can add a page too."
	homeText      = "Open a project workspace to see its wiki."
)

type projectView struct {
	Name  string
	Pages []Page
}

type recentRow struct {
	Project string
	Page    Page
}

type hitRow struct {
	Project string
	Hit     Hit
}

type sidebarData struct {
	Home       bool
	Projects   []projectView
	Recent     []recentRow
	Query      string
	Hits       []hitRow
	AgentNotes bool
	Selected   string
	Collapsed  map[string]bool
}

type mainData struct {
	Home        bool
	Project     string
	Page        *Page
	HTML        string
	Path        string
	Kind        string
	Rev         int
	ShowHistory bool
	History     []Change
	Now         time.Time
}

func pageID(project, page string) string {
	return project + "/" + page
}

func splitID(id string) (string, string, bool) {
	return strings.Cut(id, "/")
}

func sidebarView(d sidebarData) sdui.Node {
	if d.Home {
		return sdui.VStack(sdui.EmptyState(homeText, sdui.EmptyProps{Icon: "book.closed"}))
	}
	search := sdui.WithAction(sdui.WithLive(sdui.TextField(searchNode, d.Query, "Search wiki")), "search")
	items := pageItems(d)
	var body sdui.Node
	switch {
	case len(items) > 0:
		body = sdui.Tree(pagesNode, items, d.Selected)
	case d.Query != "":
		body = sdui.Empty("No pages match")
	default:
		body = sdui.Empty("No pages yet")
	}
	return sdui.VStack(sidebarHead(d), search, sdui.Scroll(sdui.VStack(body)))
}

func sidebarHead(d sidebarData) sdui.Node {
	title := sdui.Node{Type: "text", Props: map[string]any{"text": "Wiki", "style": "strong"}}
	add := sdui.WithIconOnly(sdui.WithVariant(newButton(), sdui.VariantGhost))
	head := []sdui.Node{title}
	if n := pageCount(d.Projects); n > 0 {
		head = append(head, sdui.MutedText(plural(n, "page")))
	}
	return sdui.HStack(append(head, sdui.Spacer(), add)...)
}

func pageCount(projects []projectView) int {
	n := 0
	for _, p := range projects {
		for _, pg := range p.Pages {
			if !pg.Shadow {
				n++
			}
		}
	}
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func pageItems(d sidebarData) []sdui.TreeItem {
	if d.Query != "" {
		items := make([]sdui.TreeItem, 0, len(d.Hits))
		for _, h := range d.Hits {
			items = append(items, sdui.TreeItem{ID: pageID(h.Project, h.Hit.Page), Label: h.Hit.Title, Icon: pageIcon(h.Hit.Page), Truncate: "tail"})
		}
		return items
	}
	var groups []sdui.TreeItem
	for _, p := range d.Projects {
		notesOpen := d.AgentNotes || strings.HasPrefix(d.Selected, pageID(p.Name, shadowDir+"/"))
		items := folderItems(p.Name, "", p.Pages, d.Collapsed, notesOpen)
		if len(d.Projects) == 1 {
			return items
		}
		if len(items) == 0 {
			continue
		}
		id := projectPrefix + p.Name
		g := sdui.TreeItem{ID: id, Label: p.Name, Icon: "book.closed", HasChildren: true, Expanded: !d.Collapsed[id], Action: true}
		if g.Expanded {
			g.Children = items
		}
		groups = append(groups, g)
	}
	return groups
}

func folderItems(project, dir string, pages []Page, collapsed map[string]bool, notesOpen bool) []sdui.TreeItem {
	byDir := map[string][]Page{}
	var leaves []sdui.TreeItem
	for _, pg := range pages {
		rest := strings.TrimPrefix(pg.Path, dir)
		if sub, _, ok := strings.Cut(rest, "/"); ok {
			byDir[sub] = append(byDir[sub], pg)
			continue
		}
		leaves = append(leaves, pageItem(project, pg))
	}
	dirs := make([]string, 0, len(byDir))
	for name := range byDir {
		dirs = append(dirs, name)
	}
	slices.Sort(dirs)
	items := make([]sdui.TreeItem, 0, len(dirs)+len(leaves))
	var notes []sdui.TreeItem
	for _, name := range dirs {
		full := dir + name
		id := dirPrefix + pageID(project, full)
		g := sdui.TreeItem{ID: id, Label: name, HasChildren: true, Expanded: !collapsed[id], Action: true}
		if full == shadowDir {
			g.Label, g.Expanded = notesLabel, notesOpen
		}
		if !g.Expanded {
			g.Detail = strconv.Itoa(len(byDir[name]))
		} else {
			g.Children = folderItems(project, full+"/", byDir[name], collapsed, notesOpen)
		}
		if full == shadowDir {
			notes = append(notes, g)
			continue
		}
		items = append(items, g)
	}
	if dir == "" {
		return append(append(leaves, items...), notes...)
	}
	return append(append(items, leaves...), notes...)
}

func pageItem(project string, pg Page) sdui.TreeItem {
	it := sdui.TreeItem{ID: pageID(project, pg.Path), Label: pg.Title, Icon: pageIcon(pg.Path), Truncate: "tail"}
	if pg.Stale {
		it.Badges = []sdui.HeaderBadge{{Text: staleBadge, Tone: "warning"}}
	}
	return it
}

func pageIcon(page string) string {
	switch strings.ToLower(path.Ext(page)) {
	case ".html", ".htm":
		return "play.rectangle"
	case ".mmd":
		return "point.3.connected.trianglepath.dotted"
	}
	return "doc.text"
}

func newButton() sdui.Node {
	return sdui.WithAction(sdui.Button(newNode, "New page", "plus"), "new")
}

func mainView(d mainData) sdui.Node {
	if d.Home {
		return sdui.EmptyState(homeText, sdui.EmptyProps{Icon: "book.closed", Center: true})
	}
	if d.Page == nil {
		action := sdui.WithVariant(newButton(), sdui.VariantPrimary)
		return sdui.EmptyState(emptyText, sdui.EmptyProps{Title: "No wiki pages yet", Icon: "book.closed", Action: &action, Center: true})
	}
	p := *d.Page
	actions := []sdui.Node{
		sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(editNode, "Edit", "pencil"), "edit"), sdui.VariantPrimary), "e"),
		sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(historyNode, "History", "clock.arrow.circlepath"), "history"), sdui.VariantSecondary), "cmd+y"),
		sdui.WithOverflow(sdui.WithCopy(sdui.Button(copyNode, "Copy path", "doc.on.doc"), d.Path)),
		sdui.WithOverflow(sdui.WithAction(sdui.Button(revealNode, "Reveal in Finder", "folder"), "reveal")),
		sdui.WithOverflow(sdui.WithConfirm(sdui.WithVariant(sdui.WithAction(sdui.Button(deleteNode, "Delete", "trash"), "delete"), sdui.VariantDanger), "Delete "+p.Path+"? It stays in the wiki's git history.")),
	}
	if d.ShowHistory {
		actions[1] = sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(historyNode, "Back to page", "chevron.left"), "history"), sdui.VariantSecondary), "cmd+y")
	}
	props := sdui.HeaderProps{Icon: pageIcon(p.Path), Meta: meta(p.Updated, p.By, d.Now)}
	if p.Stale {
		props.Badges = []sdui.HeaderBadge{{Text: staleBadge, Tone: "warning"}}
	}
	header := sdui.WithID(sdui.Header(p.Title, props, actions...), headerNode)
	if d.ShowHistory {
		return sdui.VStack(header, historyView(d.History))
	}
	return sdui.VStack(header, sdui.Document(docNode, sdui.DocumentProps{HTML: d.HTML, Path: d.Path, Rev: d.Rev, Size: sdui.SizeFill, Kind: d.Kind}))
}

func historyView(changes []Change) sdui.Node {
	if len(changes) == 0 {
		return sdui.Empty("No changes yet")
	}
	items := make([]sdui.Item, 0, len(changes))
	for i, c := range changes {
		items = append(items, sdui.Item{ID: strconv.Itoa(i), Label: c.Summary, Detail: c.At.Local().Format(timeLayout) + " · " + c.By, Truncate: "tail"})
	}
	return sdui.List(changesNode, items, "")
}

var leadingH1 = regexp.MustCompile(`(?s)^\s*<h1([^>]*)>(.*?)</h1>\s*`)

var idAttr = regexp.MustCompile(`\sid="([^"]*)"`)

var tags = regexp.MustCompile(`<[^>]*>`)

func dropTitle(body, title string) string {
	m := leadingH1.FindStringSubmatchIndex(body)
	if m == nil {
		return body
	}
	if strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(body[m[4]:m[5]], ""))) != strings.TrimSpace(title) {
		return body
	}
	if id := idAttr.FindStringSubmatch(body[m[2]:m[3]]); id != nil {
		return `<a id="` + id[1] + `"></a>` + body[m[1]:]
	}
	return body[m[1]:]
}

func meta(at time.Time, by string, now time.Time) string {
	if at.IsZero() {
		return "by " + by
	}
	at, now = at.Local(), now.Local()
	layout := "Jan 2, 2006 15:04"
	switch {
	case at.Year() == now.Year() && at.YearDay() == now.YearDay():
		layout = "15:04"
	case at.Year() == now.Year():
		layout = timeLayout
	}
	return "updated " + at.Format(layout) + " by " + by
}
