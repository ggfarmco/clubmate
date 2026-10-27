package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func TestProcessTreeView(t *testing.T) {
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 1, PPID: 0, Comm: "sh", CPU: 0.5, RSSKiB: 2048}},
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1, Comm: "nvim", CPU: 1.2, RSSKiB: 34 * 1024}},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 2, Comm: "gopls"}, Orphan: true, Duplicate: true},
	}
	assert.Equal(t, []sdui.TreeItem{{
		ID: "1", Label: "sh", Detail: "1 · 0.5% · 2 MB", HasChildren: true, Expanded: true,
		Children: []sdui.TreeItem{{
			ID: "2", Label: "nvim", Detail: "2 · 1.2% · 34 MB", HasChildren: true, Expanded: true,
			Children: []sdui.TreeItem{{ID: "3", Label: "gopls", Detail: "3 · 0.0% · 0 MB", Badge: "orphan,dup"}},
		}},
	}}, treeItems(nodes))

	toolbar := mainView(nodes, 2, true).Children[0]
	assert.Equal(t, "Kill nvim (2) and its children?", toolbar.Children[0].Props["confirm"])
	assert.Equal(t, "3 processes · 1 orphans", toolbar.Children[4].Props["text"])

	idle := mainView(nodes, 0, false).Children[0]
	assert.Len(t, idle.Children, 4)
	assert.Equal(t, "kill-orphans", idle.Children[0].ID)
}
