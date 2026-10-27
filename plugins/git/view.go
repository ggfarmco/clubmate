package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	sidebarSlot    = "git.sidebar"
	mainSlot       = "git.main"
	treeID         = "git"
	idSep          = "\x00"
	groupStaged    = "staged"
	groupChanges   = "changes"
	groupUntracked = "untracked"
	stageAction    = "stage"
	unstageAction  = "unstage"
	discardAction  = "discard"
	openDiffAction = "open-diff"
	commitAction   = "commit"
	repoField      = "repo"
	messageField   = "message"
	submitID       = "submit"
	missingDetail  = "missing"
	notGitDetail   = "not a git repository"
	maxDiffLines   = 2000
	maxLineChars   = 2000
	maxDiffBytes   = 1 << 20
	maxGroupLeaves = 1000
	maxTreeItems   = 4900
	moreID         = "more"
	noRepos        = "No repositories — add one with clubmate project add-repo"
)

var groupTitles = []struct{ id, title string }{
	{groupStaged, "Staged"},
	{groupChanges, "Changes"},
	{groupUntracked, "Untracked"},
}

type repoView struct {
	Path    string
	Branch  string
	Problem string
	Entries []gitrepo.Entry
}

type leaf struct {
	Repo  string
	Group string
	Path  string
	Orig  string
	Code  string
	Dir   bool
}

type mainModel struct {
	Sel     *leaf
	Body    sdui.Node
	Repos   []string
	FormID  string
	Busy    string
	Failure string
}

func (l leaf) id() string {
	return l.Repo + idSep + l.Group + idSep + l.Path
}

func (l leaf) discardable() bool {
	return l.Group == groupChanges && l.Code != "A"
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
			out = append(out, leaf{Repo: r.Path, Group: groupChanges, Path: e.Path, Code: string(e.Y)})
		}
	}
	return out
}

func sidebarView(repos []repoView, selected string, folded map[string]bool) sdui.Node {
	if len(repos) == 0 {
		return sdui.Empty(noRepos)
	}
	budget := maxTreeItems - len(repos)*(1+2*len(groupTitles))
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
			var group []leaf
			for _, l := range all {
				if l.Group == g.id {
					group = append(group, l)
				}
			}
			if len(group) == 0 {
				continue
			}
			id := r.Path + idSep + g.id
			shown := min(len(group), maxGroupLeaves, max(budget, 0))
			budget -= shown
			children := make([]sdui.TreeItem, 0, shown+1)
			for _, l := range group[:shown] {
				children = append(children, sdui.TreeItem{ID: l.id(), Label: l.rel(), Badge: l.Code})
			}
			if more := len(group) - shown; more > 0 {
				children = append(children, sdui.TreeItem{ID: id + idSep + moreID, Label: fmt.Sprintf("… %d more", more)})
			}
			root.Children = append(root.Children, sdui.TreeItem{
				ID:          id,
				Label:       fmt.Sprintf("%s (%d)", g.title, len(group)),
				HasChildren: true,
				Expanded:    !folded[id],
				Children:    children,
			})
		}
		root.HasChildren = len(root.Children) > 0
		items = append(items, root)
	}
	return sdui.Scroll(sdui.Tree(treeID, items, selected))
}

func mainView(m mainModel) sdui.Node {
	var nodes []sdui.Node
	if m.Failure != "" {
		nodes = append(nodes, sdui.Badge(m.Failure, "danger"))
	}
	if m.Busy != "" {
		nodes = append(nodes, sdui.MutedText(m.Busy))
	}
	if sel := m.Sel; sel != nil {
		rel := sel.rel()
		toolbar := []sdui.Node{mono(rel)}
		if sel.Group == groupStaged {
			toolbar = append(toolbar, sdui.Button(unstageAction, "Unstage", ""))
		} else {
			toolbar = append(toolbar, sdui.Button(stageAction, "Stage", ""))
		}
		if sel.discardable() {
			discard := sdui.WithAction(sdui.Button(sel.id(), "Discard…", ""), discardAction)
			toolbar = append(toolbar, sdui.WithConfirm(discard, "Discard changes to "+rel+"?"))
		}
		if !sel.Dir {
			toolbar = append(toolbar, sdui.Button(openDiffAction, "Open Diff in Neovim", ""))
		}
		nodes = append(nodes, sdui.HStack(toolbar...), sdui.Scroll(m.Body))
	}
	var fields []sdui.Node
	if len(m.Repos) > 1 {
		options := make([]sdui.Option, len(m.Repos))
		for i, r := range m.Repos {
			options[i] = sdui.Option{Value: r, Label: filepath.Base(r)}
		}
		value := m.Repos[0]
		if m.Sel != nil {
			value = m.Sel.Repo
		}
		fields = append(fields, sdui.Select(repoField, options, value))
	}
	fields = append(fields, sdui.TextField(messageField, "", "Commit message"), sdui.Button(submitID, "Commit", ""))
	return sdui.VStack(append(nodes, sdui.WithAction(sdui.Form(m.FormID, fields...), commitAction))...)
}

func bodyNode(text string, truncated, dir bool, err error) sdui.Node {
	switch {
	case err != nil:
		return sdui.MutedText(err.Error())
	case dir:
		return mono(truncateDiff(text, truncated))
	}
	return sdui.Diff(truncateDiff(text, truncated))
}

func mono(text string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": "mono"}}
}

func truncateDiff(text string, truncated bool) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	more := len(lines) - maxDiffLines
	if !truncated && more <= 0 && !slices.ContainsFunc(lines, func(l string) bool { return len(l) > maxLineChars }) {
		return text
	}
	lines = lines[:min(len(lines), maxDiffLines)]
	for i, l := range lines {
		lines[i] = capLine(l)
	}
	switch {
	case truncated:
		lines = append(lines, "… diff truncated (over 1 MiB)")
	case more > 0:
		lines = append(lines, fmt.Sprintf("… diff truncated (%d more lines)", more))
	}
	return strings.Join(lines, "\n")
}

func capLine(l string) string {
	n := 0
	for i := range l {
		if n == maxLineChars {
			return l[:i] + " …"
		}
		n++
	}
	return l
}

func relPath(repo, path string) string {
	if rel, err := filepath.Rel(repo, path); err == nil {
		return rel
	}
	return path
}
