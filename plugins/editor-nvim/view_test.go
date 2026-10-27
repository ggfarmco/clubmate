package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
