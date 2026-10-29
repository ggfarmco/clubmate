package main

import "clubmate/pkg/sdui"

const (
	sidebarSlot = "editor-nvim.sidebar"
	treeID      = "files"
	noRepos     = "No repositories — add one with clubmate workspace add-folder"
)

func treeView(items []sdui.TreeItem, selected string, breadcrumb []sdui.Item) sdui.Node {
	if len(items) == 0 && len(breadcrumb) == 0 {
		return sdui.Empty(noRepos)
	}
	return sdui.Scroll(sdui.BreadcrumbTree(treeID, items, selected, breadcrumb))
}
