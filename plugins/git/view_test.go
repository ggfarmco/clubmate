package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

func TestLeafMenu(t *testing.T) {
	changes := leaf{Repo: "/r", Group: groupChanges, Path: "/r/b.go", Code: "M", Restorable: true}
	assert.Equal(t, []sdui.MenuEntry{
		{Action: stageAction, Label: "Stage"},
		{Action: discardAction, Label: "Discard", Confirm: "Discard changes to b.go?"},
	}, leafMenu(changes))
	assert.Equal(t, []sdui.MenuEntry{{Action: unstageAction, Label: "Unstage"}}, leafMenu(leaf{Repo: "/r", Group: groupStaged, Path: "/r/b.go"}))
}

func TestSidebarCapsGroups(t *testing.T) {
	repo := "/r"
	entries := make([]gitrepo.Entry, 1500)
	for i := range entries {
		entries[i] = gitrepo.Entry{Path: fmt.Sprintf("/r/f%04d", i), X: ' ', Y: 'M'}
	}
	tree := changesView([]repoView{{Path: repo, Entries: entries}}, "", nil)
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
	assert.NoError(t, internalsdui.Validate(changesView([]repoView{{Path: repo, Entries: full}, {Path: "/s", Entries: full}}, "", nil)))
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

func TestReviewCollapseOthersKeepsTheFilesAncestors(t *testing.T) {
	r := &review{head: "/w", base: "main", folded: map[string]bool{}}
	for _, p := range []string{"a/b/x.go", "a/c/y.go", "d/z.go", "top.go"} {
		r.changes = append(r.changes, gitrepo.Change{Path: p, Status: "M"})
	}
	r.selected = "d/z.go"

	r.collapseOthers("a/b/x.go", rows(r))
	assert.Equal(t, map[string]bool{"a/c": true, "d": true}, r.folded)

	r.folded = map[string]bool{}
	r.collapseOthers("", rows(r))
	assert.Equal(t, map[string]bool{"a": true, "a/b": true, "a/c": true}, r.folded)
}

func TestReviewTreeStaysWithinNodeLimit(t *testing.T) {
	r := &review{head: "/w", base: "main", folded: map[string]bool{}}
	for i := range 6000 {
		r.changes = append(r.changes, gitrepo.Change{Path: fmt.Sprintf("d%04d/f.go", i), Status: "M"})
	}
	tree := reviewView(r)
	require.NoError(t, internalsdui.Validate(tree))
	items := tree.Children[0].Props["items"].([]sdui.TreeItem)
	assert.Equal(t, sdui.TreeItem{ID: moreRowID, Label: "… 3551 more", Muted: true}, items[len(items)-1])
}

func TestGeneratedFilterHidesRootAndNestedFiles(t *testing.T) {
	r := &review{head: "/w", base: "main", filters: filters{Generated: true}, folded: map[string]bool{}}
	for _, p := range []string{"go.sum", "tools/go.mod", "web/package-lock.json", "main.go", "go.summary"} {
		r.changes = append(r.changes, gitrepo.Change{Path: p, Status: "M"})
	}
	var paths []string
	for _, row := range rows(r) {
		paths = append(paths, row.Path)
	}
	assert.Equal(t, []string{"main.go", "go.summary"}, paths)
}
