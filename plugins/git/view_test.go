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
	changes := &leaf{Repo: repo, Group: groupChanges, Path: "/r/b.go", Code: "M", Restorable: true}
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

func rows(r *review) []reviewRow {
	out, _ := r.rows()
	return out
}

func TestReviewTreeCompactsAndFocuses(t *testing.T) {
	r := &review{head: "/w", base: "main", mode: moduleMode, folded: map[string]bool{}, entries: []reviewEntry{
		{Path: "a/b/x.go", Status: "M", Additions: 2, Deletions: 1, Reviewed: true, Comments: 3},
		{Path: "a/b/y.go", Status: "A", Additions: 1},
		{Path: "z.go", Status: "D", Deletions: 4},
	}}
	tree := reviewView(r)
	require.NoError(t, internalsdui.Validate(tree))
	items := tree.Children[0].Props["items"].([]sdui.TreeItem)
	require.Len(t, items, 2)
	assert.Equal(t, "a / b", items[0].Label)
	assert.Equal(t, "a/b", items[0].ID)
	assert.Equal(t, sdui.TreeItem{ID: "a/b/x.go", Label: "x.go", Badge: "M", Additions: 2, Deletions: 1, Reviewed: true, Comments: 3}, items[0].Children[0])
	assert.Equal(t, "3 files · 1/3 reviewed", tree.Children[0].Props["review"].(reviewHeader).Progress)

	require.NoError(t, r.setFocus("a/b/y.go", rows(r)))
	tree = reviewView(r)
	items = tree.Children[0].Props["items"].([]sdui.TreeItem)
	assert.Equal(t, []string{"a/b/x.go", "a/b/y.go"}, []string{items[0].ID, items[1].ID})
	assert.Equal(t, []sdui.Item{{ID: ".", Label: "w"}, {ID: "a", Label: "a"}, {ID: "a/b", Label: "b"}}, tree.Children[0].Props["breadcrumb"])
}
