package main

import (
	"fmt"
	"path/filepath"
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
	maxDiffLines   = 2000
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
	Missing bool
	Entries []gitrepo.Entry
}

type leaf struct {
	Repo  string
	Group string
	Path  string
	Orig  string
	Code  string
}

func (l leaf) id() string {
	return l.Repo + idSep + l.Group + idSep + l.Path
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

func sidebarView(repos []repoView, selected string) sdui.Node {
	if len(repos) == 0 {
		return sdui.Empty(noRepos)
	}
	items := make([]sdui.TreeItem, 0, len(repos))
	for _, r := range repos {
		root := sdui.TreeItem{ID: r.Path, Label: filepath.Base(r.Path), Detail: r.Branch, Expanded: true}
		if r.Missing {
			root.Detail = missingDetail
			items = append(items, root)
			continue
		}
		all := leaves(r)
		for _, g := range groupTitles {
			var children []sdui.TreeItem
			for _, l := range all {
				if l.Group == g.id {
					children = append(children, sdui.TreeItem{ID: l.id(), Label: l.rel(), Badge: l.Code})
				}
			}
			if len(children) > 0 {
				root.Children = append(root.Children, sdui.TreeItem{
					ID:          r.Path + idSep + g.id,
					Label:       fmt.Sprintf("%s (%d)", g.title, len(children)),
					HasChildren: true,
					Expanded:    true,
					Children:    children,
				})
			}
		}
		root.HasChildren = len(root.Children) > 0
		items = append(items, root)
	}
	return sdui.Scroll(sdui.Tree(treeID, items, selected))
}

func mainView(sel *leaf, body sdui.Node, repos []string, formID string) sdui.Node {
	var nodes []sdui.Node
	if sel != nil {
		rel := sel.rel()
		toolbar := []sdui.Node{{Type: "text", Props: map[string]any{"text": rel, "style": "mono"}}}
		if sel.Group == groupStaged {
			toolbar = append(toolbar, sdui.Button(unstageAction, "Unstage", ""))
		} else {
			toolbar = append(toolbar, sdui.Button(stageAction, "Stage", ""))
		}
		if sel.Group == groupChanges {
			toolbar = append(toolbar, sdui.WithConfirm(sdui.Button(discardAction, "Discard…", ""), "Discard changes to "+rel+"?"))
		}
		toolbar = append(toolbar, sdui.Button(openDiffAction, "Open Diff in Neovim", ""))
		nodes = append(nodes, sdui.HStack(toolbar...), sdui.Scroll(body))
	}
	var fields []sdui.Node
	if len(repos) > 1 {
		options := make([]sdui.Option, len(repos))
		for i, r := range repos {
			options[i] = sdui.Option{Value: r, Label: filepath.Base(r)}
		}
		value := repos[0]
		if sel != nil {
			value = sel.Repo
		}
		fields = append(fields, sdui.Select(repoField, options, value))
	}
	fields = append(fields, sdui.TextField(messageField, "", "Commit message"), sdui.Button(submitID, "Commit", ""))
	return sdui.VStack(append(nodes, sdui.WithAction(sdui.Form(formID, fields...), commitAction))...)
}

func diffNode(text string, err error) sdui.Node {
	if err != nil {
		return sdui.MutedText(err.Error())
	}
	return sdui.Diff(truncateDiff(text))
}

func truncateDiff(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) <= maxDiffLines {
		return text
	}
	return strings.Join(lines[:maxDiffLines], "\n") + "\n" + fmt.Sprintf("… diff truncated (%d more lines)", len(lines)-maxDiffLines)
}

func relPath(repo, path string) string {
	if rel, err := filepath.Rel(repo, path); err == nil {
		return rel
	}
	return path
}
