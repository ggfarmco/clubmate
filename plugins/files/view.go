package main

import "clubmate/pkg/sdui"

const (
	sidebarSlot = "editor-nvim.sidebar"
	treeID      = "files"
	noRepos     = "No repositories — add one with clubmate workspace add-folder"
)

func treeView(items []sdui.TreeItem, selected string) sdui.Node {
	if len(items) == 0 {
		return sdui.Empty(noRepos)
	}
	return sdui.Scroll(sdui.Tree(treeID, items, selected))
}
