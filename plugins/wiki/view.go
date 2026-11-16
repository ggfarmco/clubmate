package main

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	sidebarSlot    = "wiki.sidebar"
	mainSlot       = "wiki.main"
	searchNode     = "search"
	recentNode     = "recent"
	pagesNode      = "pages"
	agentNotesNode = "agentNotes"
	headerNode     = "header"
	docNode        = "page"
	changesNode    = "changes"
	newNode        = "new"
	editNode       = "edit"
	historyNode    = "history"
	deleteNode     = "delete"
	dirPrefix      = "dir:"
	projectPrefix  = "project:"
	staleBadge     = "stale"
	timeLayout     = "Jan 2 15:04"
	emptyText      = "Agents write here as they work: how things work, why they were decided, examples. You can add a page too."
	homeText       = "Open a project workspace to see its wiki."
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
	var body []sdui.Node
	if d.Query == "" && len(d.Recent) > 0 {
		items := make([]sdui.Item, 0, len(d.Recent))
		for _, r := range d.Recent {
			items = append(items, sdui.Item{ID: pageID(r.Project, r.Page.Path), Label: r.Page.Title, Detail: r.Page.By, Icon: pageIcon(r.Page.Path), Truncate: "tail"})
		}
		body = append(body, sdui.MutedText("Recent"), sdui.List(recentNode, items, ""))
	}
	items := pageItems(d)
	switch {
	case len(items) > 0:
		if d.Query == "" && len(d.Recent) > 0 {
			body = append(body, sdui.MutedText("Pages"))
		}
		body = append(body, sdui.Tree(pagesNode, items, d.Selected))
	case d.Query != "":
		body = append(body, sdui.Empty("No pages match"))
	default:
		body = append(body, sdui.Empty("No pages yet"))
	}
	notes := sdui.SettingRow("Show agent notes", "", sdui.WithAction(sdui.Toggle(agentNotesNode, d.AgentNotes), "agentNotes"))
	return sdui.VStack(search, sdui.Scroll(sdui.VStack(body...)), notes)
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
		pages := p.Pages
		if !d.AgentNotes {
			pages = slices.DeleteFunc(slices.Clone(pages), func(pg Page) bool { return pg.Shadow })
		}
		items := folderItems(p.Name, "", pages, d.Collapsed)
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

func folderItems(project, dir string, pages []Page, collapsed map[string]bool) []sdui.TreeItem {
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
		label, icon := name, "folder"
		if full == shadowDir {
			label, icon = "Agent notes", "lock"
		}
		g := sdui.TreeItem{ID: id, Label: label, Icon: icon, HasChildren: true, Expanded: !collapsed[id], Action: true}
		if g.Expanded {
			g.Children = folderItems(project, full+"/", byDir[name], collapsed)
		}
		if full == shadowDir {
			notes = append(notes, g)
			continue
		}
		items = append(items, g)
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
	historyLabel := "History"
	if d.ShowHistory {
		historyLabel = "Back to page"
	}
	actions := []sdui.Node{
		sdui.WithVariant(sdui.WithAction(sdui.Button(editNode, "Edit in nvim", "pencil"), "edit"), sdui.VariantSecondary),
		sdui.WithVariant(sdui.WithAction(sdui.Button(historyNode, historyLabel, "clock.arrow.circlepath"), "history"), sdui.VariantGhost),
		sdui.WithVariant(newButton(), sdui.VariantGhost),
		sdui.WithConfirm(sdui.WithVariant(sdui.WithAction(sdui.Button(deleteNode, "Delete", "trash"), "delete"), sdui.VariantDanger), "Delete "+p.Path+"? It stays in the wiki's git history."),
	}
	props := sdui.HeaderProps{Icon: pageIcon(p.Path), Meta: meta(p.Updated, p.By)}
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

func meta(at time.Time, by string) string {
	if at.IsZero() {
		return "by " + by
	}
	return "updated " + at.Local().Format(timeLayout) + " by " + by
}
