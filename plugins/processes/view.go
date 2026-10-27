package main

import (
	"fmt"
	"slices"
	"strconv"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	allID       = "all"
	sessionsID  = "sessions"
	treeID      = "processes"
	selectPID   = "select-pid"
	killAction  = "kill"
	orphanKill  = "kill-orphans"
	autoKill    = "autokill"
	kibPerMiB   = 1024
	noSelection = "Kill the selected process and its children?"
)

type sessionRow struct {
	ID    string
	Title string
	Owner string
}

func sidebarView(sessions []sessionRow, selected string) sdui.Node {
	items := []sdui.Item{{ID: allID, Label: "All"}}
	for _, s := range sessions {
		items = append(items, sdui.Item{ID: s.ID, Label: s.Title, Detail: s.Owner})
	}
	if selected == "" {
		selected = allID
	}
	return sdui.List(sessionsID, items, selected)
}

func mainView(nodes []pluginsdk.ProcessNode, selected int, auto bool) sdui.Node {
	orphans := 0
	for _, n := range nodes {
		if n.Orphan {
			orphans++
		}
	}
	toolbar := sdui.HStack(
		withConfirm(sdui.Button(killAction, "Kill", "xmark.circle"), killConfirm(nodes, selected)),
		withConfirm(sdui.Button(orphanKill, "Kill orphans", "trash"), "Kill all orphan processes?"),
		sdui.WithAction(sdui.Toggle(autoKill, auto), autoKill),
		sdui.Text("Auto-kill orphans"),
		sdui.MutedText(fmt.Sprintf("%d processes · %d orphans", len(nodes), orphans)),
	)
	if len(nodes) == 0 {
		return sdui.VStack(toolbar, sdui.Empty("No processes"))
	}
	selectedID := ""
	if selected != 0 {
		selectedID = strconv.Itoa(selected)
	}
	tree := sdui.WithAction(sdui.Tree(treeID, treeItems(nodes), selectedID), selectPID)
	return sdui.VStack(toolbar, sdui.Scroll(tree))
}

func treeItems(nodes []pluginsdk.ProcessNode) []sdui.TreeItem {
	present := make(map[int]bool, len(nodes))
	children := make(map[int][]pluginsdk.ProcessNode, len(nodes))
	for _, n := range nodes {
		present[n.Process.PID] = true
		children[n.Process.PPID] = append(children[n.Process.PPID], n)
	}
	var build func(n pluginsdk.ProcessNode) sdui.TreeItem
	build = func(n pluginsdk.ProcessNode) sdui.TreeItem {
		item := sdui.TreeItem{
			ID:     strconv.Itoa(n.Process.PID),
			Label:  n.Process.Comm,
			Detail: fmt.Sprintf("%d · %.1f%% · %d MB", n.Process.PID, n.Process.CPU, n.Process.RSSKiB/kibPerMiB),
			Badge:  badge(n),
		}
		for _, c := range children[n.Process.PID] {
			item.Children = append(item.Children, build(c))
		}
		item.HasChildren = len(item.Children) > 0
		item.Expanded = item.HasChildren
		return item
	}
	items := []sdui.TreeItem{}
	for _, n := range nodes {
		if !present[n.Process.PPID] {
			items = append(items, build(n))
		}
	}
	return items
}

func badge(n pluginsdk.ProcessNode) string {
	switch {
	case n.Orphan && n.Duplicate:
		return "orphan,dup"
	case n.Orphan:
		return "orphan"
	case n.Duplicate:
		return "dup"
	}
	return ""
}

func killConfirm(nodes []pluginsdk.ProcessNode, selected int) string {
	i := slices.IndexFunc(nodes, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == selected })
	if i < 0 {
		return noSelection
	}
	return fmt.Sprintf("Kill %s (%d) and its children?", nodes[i].Process.Comm, selected)
}

func withConfirm(n sdui.Node, text string) sdui.Node {
	props := make(map[string]any, len(n.Props)+1)
	for k, v := range n.Props {
		props[k] = v
	}
	props["confirm"] = text
	n.Props = props
	return n
}
