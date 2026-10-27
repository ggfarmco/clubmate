package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
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
	got := strings.Split(truncateDiff(strings.Join(lines, "\n")+"\n", false), "\n")
	require.Len(t, got, 2001)
	assert.Equal(t, lines[:2000], got[:2000])
	assert.Equal(t, "… diff truncated (500 more lines)", got[2000])

	binary := "Binary files a/x and b/x differ\n"
	assert.Equal(t, binary, truncateDiff(binary, false))

	long := "+" + strings.Repeat("é", 2500)
	assert.Equal(t, []string{"+" + strings.Repeat("é", 1999) + " …", "+short", "… diff truncated (over 1 MiB)"}, strings.Split(truncateDiff(long+"\n+short\n", true), "\n"))
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

func TestSidebarCapsGroups(t *testing.T) {
	repo := "/r"
	entries := make([]gitrepo.Entry, 1500)
	for i := range entries {
		entries[i] = gitrepo.Entry{Path: fmt.Sprintf("/r/f%04d", i), X: ' ', Y: 'M'}
	}
	tree := sidebarView([]repoView{{Path: repo, Entries: entries}}, "", nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := tree.Children[0].Props["items"].([]sdui.TreeItem)
	group := items[0].Children[0]
	assert.Equal(t, "Changes (1500)", group.Label)
	require.Len(t, group.Children, 1001)
	assert.Equal(t, "f0999", group.Children[999].Label)
	assert.Equal(t, sdui.TreeItem{ID: repo + idSep + groupChanges + idSep + "more", Label: "… 500 more"}, group.Children[1000])

	var full []gitrepo.Entry
	for i, e := range entries {
		full = append(full, gitrepo.Entry{Path: e.Path, X: 'M', Y: 'M'}, gitrepo.Entry{Path: fmt.Sprintf("/r/u%04d", i), X: '?', Y: '?'})
	}
	assert.NoError(t, internalsdui.Validate(sidebarView([]repoView{{Path: repo, Entries: full}, {Path: "/s", Entries: full}}, "", nil)))
}
