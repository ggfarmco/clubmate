package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func button(n sdui.Node, action string) (sdui.Node, bool) {
	if n.Type == "button" && n.Props["action"] == action {
		return n, true
	}
	for _, c := range n.Children {
		if got, ok := button(c, action); ok {
			return got, true
		}
	}
	return sdui.Node{}, false
}

func TestDiffViewTruncates(t *testing.T) {
	lines := make([]string, 2500)
	for i := range lines {
		lines[i] = fmt.Sprintf("+line %d", i)
	}
	got := strings.Split(truncateDiff(strings.Join(lines, "\n")+"\n"), "\n")
	require.Len(t, got, 2001)
	assert.Equal(t, lines[:2000], got[:2000])
	assert.Equal(t, "… diff truncated (500 more lines)", got[2000])

	binary := "Binary files a/x and b/x differ\n"
	assert.Equal(t, binary, truncateDiff(binary))
}

func TestDiscardConfirm(t *testing.T) {
	repo := "/r"
	changes := &leaf{Repo: repo, Group: groupChanges, Path: "/r/b.go"}
	d, ok := button(mainView(mainModel{Sel: changes, Repos: []string{repo}, FormID: "commit-0"}), discardAction)
	require.True(t, ok)
	assert.Equal(t, changes.id(), d.ID)
	assert.Equal(t, "Discard changes to b.go?", d.Props["confirm"])

	staged := &leaf{Repo: repo, Group: groupStaged, Path: "/r/b.go"}
	_, ok = button(mainView(mainModel{Sel: staged, Repos: []string{repo}, FormID: "commit-0"}), discardAction)
	assert.False(t, ok)
}
