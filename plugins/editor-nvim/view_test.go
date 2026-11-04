package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/shortcuttest"
)

func TestStatusView(t *testing.T) {
	n := statusView(status{Mode: "i", File: "/r/repo/a/b.go", LSP: []string{"gopls"}, Branch: "main"}, []string{"/r/other", "/r/repo"})
	var texts []string
	for _, c := range n.Children {
		texts = append(texts, c.Props["text"].(string))
	}
	assert.Equal(t, "horizontal", n.Props["direction"])
	assert.Equal(t, []string{"a/b.go", "INSERT", "gopls", "main"}, texts)
}

func TestStatusViewLeavesNvimItemsToItsStatusline(t *testing.T) {
	n := statusView(status{Mode: "n", File: "/r/repo/a.go", LSP: []string{"gopls"}, Branch: "main", Light: true, Statusline: true}, []string{"/r/repo"})
	var texts []string
	for _, c := range n.Children {
		texts = append(texts, c.Props["text"].(string))
	}
	assert.Equal(t, []string{"light", chatHint}, texts)
}

func TestStatusSendsTheCurrentReviewTab(t *testing.T) {
	n := statusView(status{Statusline: true}, nil)
	assert.Equal(t, chatHint, n.Children[len(n.Children)-1].Props["text"])
	_, ok := findNode(n, sendNode)
	assert.False(t, ok)

	n = statusView(status{Statusline: true, Review: true, Open: 2}, nil)
	assert.Equal(t, reviewHint, n.Children[len(n.Children)-1].Props["text"])
	send, ok := findNode(n, sendNode)
	require.True(t, ok)
	assert.Equal(t, []any{sendShortcut, sendAction, nil}, []any{send.Props["shortcut"], send.Props["action"], send.Props["disabled"]})
	shortcuttest.RequireDeclared(t, n)

	send, _ = findNode(statusView(status{Review: true}, nil), sendNode)
	assert.Equal(t, true, send.Props["disabled"])
}
