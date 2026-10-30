package main

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	sidebarSlot    = "git.sidebar"
	treeID         = "git"
	idSep          = "\x00"
	groupStaged    = "staged"
	groupChanges   = "changes"
	groupUntracked = "untracked"
	stageAction    = "stage"
	unstageAction  = "unstage"
	discardAction  = "discard"
	commitAction   = "commit"
	repoField      = "repo"
	messageField   = "message"
	submitID       = "submit"
	missingDetail  = "missing"
	notGitDetail   = "not a git repository"
	maxGroupLeaves = 1000
	maxTreeItems   = 4900
	moreID         = "more"
	noRepos        = "No repositories — add one with clubmate workspace add-folder"
)

var groupTitles = []struct{ id, title string }{
	{groupStaged, "Staged"},
	{groupChanges, "Changes"},
	{groupUntracked, "Untracked"},
}

type repoView struct {
	Path       string
	Branch     string
	Problem    string
	Entries    []gitrepo.Entry
	Restorable map[string]bool
}

type leaf struct {
	Repo       string
	Group      string
	Path       string
	Orig       string
	Code       string
	Unmerged   bool
	Restorable bool
}

type sidebarModel struct {
	Repos    []repoView
	Selected string
	Folded   map[string]bool
	Live     []string
	FormID   string
	Busy     string
	Failure  string
}

func (l leaf) id() string {
	return l.Repo + idSep + l.Group + idSep + l.Path
}

func (l leaf) discardable() bool {
	return l.Group == groupChanges && l.Restorable
}

func (l leaf) rel() string {
	return relPath(l.Repo, l.Path)
}

func leaves(r repoView) []leaf {
	var out []leaf
	for _, e := range r.Entries {
		if e.X == '?' && e.Y == '?' {
			out = append(out, leaf{Repo: r.Path, Group: groupUntracked, Path: e.Path, Code: "U"})
			continue
		}
		if e.X != ' ' && e.X != '?' {
			out = append(out, leaf{Repo: r.Path, Group: groupStaged, Path: e.Path, Orig: e.Orig, Code: string(e.X)})
		}
		if e.Y != ' ' && e.Y != '?' {
			out = append(out, leaf{Repo: r.Path, Group: groupChanges, Path: e.Path, Code: string(e.Y), Unmerged: unmerged(e), Restorable: r.Restorable[e.Path]})
		}
	}
	return out
}

func unmerged(e gitrepo.Entry) bool {
	pair := string([]byte{e.X, e.Y})
	return e.X == 'U' || e.Y == 'U' || pair == "AA" || pair == "DD"
}

func sidebarView(m sidebarModel) sdui.Node {
	if len(m.Repos) == 0 {
		return sdui.Empty(noRepos)
	}
	var nodes []sdui.Node
	if m.Failure != "" {
		nodes = append(nodes, sdui.Badge(m.Failure, "danger"))
	}
	if m.Busy != "" {
		nodes = append(nodes, sdui.MutedText(m.Busy))
	}
	if len(m.Live) > 0 {
		nodes = append(nodes, commitView(m))
	}
	return sdui.VStack(append(nodes, changesView(m.Repos, m.Selected, m.Folded))...)
}

func commitView(m sidebarModel) sdui.Node {
	var fields []sdui.Node
	if len(m.Live) > 1 {
		value := m.Live[0]
		if repo, _, ok := strings.Cut(m.Selected, idSep); ok && slices.Contains(m.Live, repo) {
			value = repo
		}
		fields = append(fields, sdui.Select(repoField, repoOptions(m.Live), value))
	}
	fields = append(fields, sdui.TextField(messageField, "", "Commit message"), sdui.Button(submitID, "Commit", ""))
	return sdui.WithAction(sdui.Form(m.FormID, fields...), commitAction)
}

func changesView(repos []repoView, selected string, folded map[string]bool) sdui.Node {
	budget := maxTreeItems - len(repos)*(1+2*len(groupTitles)) - 2
	items := make([]sdui.TreeItem, 0, len(repos))
	for _, r := range repos {
		root := sdui.TreeItem{ID: r.Path, Label: filepath.Base(r.Path), Detail: r.Branch, Expanded: !folded[r.Path]}
		if r.Problem != "" {
			root.Detail = r.Problem
			items = append(items, root)
			continue
		}
		all := leaves(r)
		for _, g := range groupTitles {
			if group := inGroup(all, g.id); len(group) > 0 {
				root.Children = append(root.Children, groupItem(r.Path+idSep+g.id, g.title, group, folded, &budget))
			}
		}
		root.HasChildren = len(root.Children) > 0
		items = append(items, root)
	}
	return sdui.Scroll(sdui.Tree(treeID, items, selected))
}

func inGroup(all []leaf, group string) []leaf {
	var out []leaf
	for _, l := range all {
		if l.Group == group {
			out = append(out, l)
		}
	}
	return out
}

func groupItem(id, title string, group []leaf, folded map[string]bool, budget *int) sdui.TreeItem {
	children := make([]sdui.TreeItem, 0, min(len(group), maxGroupLeaves)+1)
	for _, l := range group {
		menu := leafMenu(l)
		if len(children) == maxGroupLeaves || *budget < 1+len(menu) {
			break
		}
		*budget -= 1 + len(menu)
		children = append(children, sdui.TreeItem{ID: l.id(), Label: l.rel(), Badge: l.Code, Menu: menu})
	}
	if more := len(group) - len(children); more > 0 {
		children = append(children, sdui.TreeItem{ID: id + idSep + moreID, Label: fmt.Sprintf("… %d more", more)})
	}
	return sdui.TreeItem{
		ID:          id,
		Label:       fmt.Sprintf("%s (%d)", title, len(group)),
		HasChildren: len(children) > 0,
		Expanded:    !folded[id],
		Children:    children,
	}
}

func leafMenu(l leaf) []sdui.MenuEntry {
	if l.Group == groupStaged {
		return []sdui.MenuEntry{{Action: unstageAction, Label: "Unstage"}}
	}
	menu := []sdui.MenuEntry{{Action: stageAction, Label: "Stage"}}
	if l.discardable() {
		menu = append(menu, sdui.MenuEntry{Action: discardAction, Label: "Discard", Confirm: "Discard changes to " + l.rel() + "?"})
	}
	return menu
}

func repoOptions(repos []string) []sdui.Option {
	options := make([]sdui.Option, len(repos))
	for i, r := range repos {
		options[i] = sdui.Option{Value: r, Label: filepath.Base(r)}
	}
	return options
}

func relPath(repo, path string) string {
	if rel, err := filepath.Rel(repo, path); err == nil {
		return rel
	}
	return path
}

type reviewHeader struct {
	Base     string       `json:"base"`
	Head     string       `json:"head"`
	BaseRef  string       `json:"baseRef"`
	HeadRef  string       `json:"headRef"`
	ReadOnly bool         `json:"readOnly,omitempty"`
	Mode     string       `json:"mode"`
	Progress string       `json:"progress"`
	Warning  string       `json:"warning,omitempty"`
	Error    string       `json:"error,omitempty"`
	Chips    []reviewChip `json:"chips"`
	Started  int          `json:"started,omitempty"`
}

type reviewChip struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	On    bool   `json:"on"`
}

type dirNode struct {
	path  string
	dirs  map[string]*dirNode
	files []reviewRow
}

func reviewView(r *review) sdui.Node {
	if r == nil {
		return sdui.Empty("")
	}
	rows, hidden := r.rows()
	module := r.mode == moduleMode
	header := reviewHeader{
		Base:     cmp.Or(r.baseLabel, r.base),
		Head:     cmp.Or(r.headLabel, filepath.Base(r.head)),
		BaseRef:  r.base,
		HeadRef:  cmp.Or(r.branch, r.head),
		ReadOnly: r.branch != "",
		Mode:     cmp.Or(r.mode, nativeMode),
		Progress: progress(rows, hidden, module),
		Warning:  r.warning,
		Error:    r.err,
		Chips:    r.filters.chips(),
		Started:  r.started,
	}
	root := buildDirs(rows)
	node := root
	var crumbs []sdui.Item
	if r.focus != "" {
		if n := root.find(r.focus); n != nil {
			node = n
			crumbs = breadcrumb(header.Head, r.focus)
		}
	}
	budget := maxReviewItems - len(crumbs) - len(header.Chips)
	items, more := node.items(r.folded, module, &budget)
	if more > 0 {
		items = append(items, sdui.TreeItem{ID: moreRowID, Label: fmt.Sprintf("… %d more", more), Muted: true})
	}
	tree := sdui.BreadcrumbTree(reviewTreeID, items, r.selected, crumbs)
	tree.Props["review"] = header
	return sdui.Scroll(tree)
}

func breadcrumb(head, focus string) []sdui.Item {
	crumbs := []sdui.Item{{ID: rootCrumb, Label: head}}
	parts := strings.Split(focus, "/")
	for i := range parts {
		crumbs = append(crumbs, sdui.Item{ID: strings.Join(parts[:i+1], "/"), Label: parts[i]})
	}
	return crumbs
}

func progress(rows []reviewRow, hidden int, module bool) string {
	parts := []string{fmt.Sprintf("%d files", len(rows))}
	if len(rows) == 1 {
		parts[0] = "1 file"
	}
	if hidden > 0 {
		parts = append(parts, fmt.Sprintf("%d hidden", hidden))
	}
	if module {
		done := 0
		for _, row := range rows {
			if row.Reviewed {
				done++
			}
		}
		parts = append(parts, fmt.Sprintf("%d/%d reviewed", done, len(rows)))
	}
	return strings.Join(parts, " · ")
}

func buildDirs(rows []reviewRow) *dirNode {
	root := &dirNode{path: rootCrumb, dirs: map[string]*dirNode{}}
	for _, row := range rows {
		n := root
		parts := strings.Split(row.Path, "/")
		for i, part := range parts[:len(parts)-1] {
			child := n.dirs[part]
			if child == nil {
				child = &dirNode{path: strings.Join(parts[:i+1], "/"), dirs: map[string]*dirNode{}}
				n.dirs[part] = child
			}
			n = child
		}
		n.files = append(n.files, row)
	}
	return root
}

func (n *dirNode) find(path string) *dirNode {
	for part := range strings.SplitSeq(path, "/") {
		if n = n.dirs[part]; n == nil {
			return nil
		}
	}
	return n
}

func (n *dirNode) items(folded map[string]bool, module bool, budget *int) ([]sdui.TreeItem, int) {
	var items []sdui.TreeItem
	skipped := 0
	for _, name := range slices.Sorted(maps.Keys(n.dirs)) {
		child, label := n.dirs[name], name
		for len(child.files) == 0 && len(child.dirs) == 1 {
			for only, next := range child.dirs {
				child, label = next, label+chainSeparator+only
			}
		}
		if *budget <= 0 {
			skipped += child.size()
			continue
		}
		*budget--
		children, more := child.items(folded, module, budget)
		skipped += more
		items = append(items, sdui.TreeItem{ID: child.path, Label: label, HasChildren: true, Expanded: !folded[child.path], Children: children})
	}
	for _, f := range n.sortedFiles() {
		if *budget <= 0 {
			skipped++
			continue
		}
		*budget--
		item := sdui.TreeItem{ID: f.Path, Label: filepath.Base(f.Path), Badge: f.Status, Additions: f.Additions, Deletions: f.Deletions}
		if module {
			item.Reviewed, item.Comments = f.Reviewed, f.Comments
		}
		items = append(items, item)
	}
	return items, skipped
}

func (n *dirNode) size() int {
	total := len(n.files)
	for _, d := range n.dirs {
		total += d.size()
	}
	return total
}

func (n *dirNode) order() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(n.dirs)) {
		out = append(out, n.dirs[name].order()...)
	}
	for _, f := range n.sortedFiles() {
		out = append(out, f.Path)
	}
	return out
}

func (n *dirNode) sortedFiles() []reviewRow {
	return slices.SortedFunc(slices.Values(n.files), func(a, b reviewRow) int { return strings.Compare(a.Path, b.Path) })
}

func (f filters) chips() []reviewChip {
	chips := []reviewChip{{ID: "tests", Label: "Tests", On: f.Tests}, {ID: "generated", Label: "Generated", On: f.Generated}}
	if f.Glob != "" {
		chips = append(chips, reviewChip{ID: "glob", Label: f.Glob, On: true})
	}
	return chips
}
