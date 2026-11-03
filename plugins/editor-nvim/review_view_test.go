package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func grammarReview() *review {
	r := newReview("ws", "/repo", reviewKey{Worktree: "/repo", Base: "main"}, 1, &fakeReviewer{})
	r.Info = reviewInfo{Labels: reviewLabels{Left: "Before turn 3", Right: "Working tree"}}
	r.OpenerTitle = "Show missed cron runs"
	r.Counts = reviewCategories{Tests: 1, Generated: 1, HideTests: true, HideGenerated: true}
	r.Entries = []reviewEntry{
		{Path: "pkg/sdui/node.go", Kind: "modified", Category: "code", Additions: 8, Deletions: 7, Comments: 1},
		{Path: "pkg/sdui/new.go", Kind: "new", Category: "code", Additions: 10},
		{Path: "pkg/sdui/gone.go", Kind: "deleted", Category: "code", Deletions: 4, Reviewed: true},
		{Path: "docs/moved.md", Kind: "renamed", OldPath: "docs/old.md", Category: "code", Reviewed: true, Stale: true},
		{Path: "README.md", Kind: "modified", Category: "code", Additions: 1, Deletions: 1},
		{Path: "pkg/sdui/node_test.go", Kind: "modified", Category: "test", Additions: 3},
		{Path: "gen/api.pb.go", Kind: "modified", Category: "generated", Additions: 90},
	}
	return r
}

func TestReviewRowGrammar(t *testing.T) {
	r := grammarReview()
	r.Current = "pkg/sdui/new.go"
	r.Comments = []reviewComment{
		{ID: 7, File: "pkg/sdui/node.go", LineStart: 23, Type: "issue", Text: "Workspace is set only by the Always allow path.\nSecond line", Status: "open", Side: "new"},
		{ID: 3, File: "pkg/sdui/node.go", LineStart: 17, Type: "suggestion", Text: "align with PermissionCardView", Status: "resolved"},
	}
	n := reviewView([]*review{r}, r.ID)
	require.NoError(t, internalsdui.Validate(n))

	node := findItem(t, n, "review/tree", "file:pkg/sdui/node.go")
	assert.Equal(t, "node.go", node.Label)
	assert.Equal(t, "circle", node.Icon)
	assert.Equal(t, []sdui.HeaderBadge{{Text: "1", Tone: "warning"}}, node.Badges)
	assert.Equal(t, &sdui.Changes{Added: 8, Removed: 7}, node.Changes)
	assert.Equal(t, []sdui.MenuEntry{{Action: "toggleReviewed", Label: "Mark reviewed", Key: "r"}}, node.Menu)

	newFile := findItem(t, n, "review/tree", "file:pkg/sdui/new.go")
	assert.Equal(t, "arrowtriangle.right.fill", newFile.Icon)
	assert.Equal(t, []sdui.HeaderBadge{{Text: "new", Tone: "success"}}, newFile.Badges)
	gone := findItem(t, n, "review/tree", "file:pkg/sdui/gone.go")
	assert.Equal(t, "checkmark", gone.Icon)
	assert.Equal(t, []sdui.HeaderBadge{{Text: "deleted", Tone: "danger"}}, gone.Badges)
	assert.Equal(t, "Mark not reviewed", gone.Menu[0].Label)
	moved := findItem(t, n, "review/tree", "file:docs/moved.md")
	assert.Equal(t, []string{staleIcon, "warning"}, []string{moved.Icon, moved.Tone})
	assert.Equal(t, []sdui.HeaderBadge{{Text: "renamed", Tone: "neutral"}}, moved.Badges)
	readme := findItem(t, n, "review/tree", "file:README.md")
	assert.Empty(t, readme.Badges)

	dir := findItem(t, n, "review/tree", "dir:pkg/sdui")
	assert.Equal(t, "1/3", dir.Detail)
	assert.Equal(t, []sdui.HeaderBadge{{Text: "1", Tone: "warning"}}, dir.Badges)
	tree, _ := findNode(n, "review/tree")
	assert.NotContains(t, texts(tree), "node_test.go")

	header, _ := findNode(n, "review/header")
	assert.Equal(t, []string{"1 / 5 reviewed", "+19", "−12", "·", "1 open comment", "Show", "code", "tests 1", "generated 1", "reviewed"}, texts(header))
	filters, _ := findNode(n, "review/filters")
	assert.Len(t, filters.Children, 2)
	assert.Equal(t, "Show missed cron runs", header.Children[0].Props["meta"])

	comments := findItem(t, n, "review/tree", "comments")
	assert.Equal(t, "Comments · 1 open", comments.Label)
	assert.Equal(t, []string{"c:7", "c:3"}, []string{comments.Children[0].ID, comments.Children[1].ID})
	assert.Equal(t, "node.go:23 · Workspace is set only by the Always allow path.", comments.Children[0].Label)
	assert.Equal(t, []string{"issue", staleIcon, "warning"}, []string{comments.Children[0].Detail, comments.Children[0].Icon, comments.Children[0].Tone})
	assert.Equal(t, []any{"resolved", true}, []any{comments.Children[1].Detail, comments.Children[1].Faint})

	footer, _ := findNode(n, "review/footer")
	assert.Equal(t, []string{reviewKeys, "Send 1 comment", "Copy ,re", reviewLegend}, texts(footer))
	strip, _ := findNode(n, "review/strip")
	assert.Equal(t, []string{"Review", "1/5", "1 open", "Previous file", "new.go 2 of 5", "Next file"}, texts(strip))
	_, tip := findNode(n, "review/tip")
	assert.False(t, tip)
}

func TestReviewFirstTipAndDoneCopy(t *testing.T) {
	r := grammarReview()
	for i := range r.Entries {
		r.Entries[i].Reviewed, r.Entries[i].Stale, r.Entries[i].Comments = false, false, 0
	}
	n := reviewView([]*review{r}, r.ID)
	tip, ok := findNode(n, "review/tip")
	require.True(t, ok)
	assert.Equal(t, []string{"Read a file, press r to mark it reviewed; ]f opens the next one. Tests and generated files are hidden."}, texts(tip))
	header, _ := findNode(n, "review/header")
	assert.Contains(t, texts(header), "no comments")

	for i := range r.Entries {
		r.Entries[i].Reviewed = true
	}
	r.Comments = []reviewComment{{ID: 1, Status: "open", Type: "issue"}, {ID: 2, Status: "open", Type: "question"}, {ID: 3, Status: "open", Type: "issue"}}
	n = reviewView([]*review{r}, r.ID)
	header, _ = findNode(n, "review/header")
	assert.Contains(t, texts(header), "All 5 files reviewed · 3 comments for Show missed cron runs")
	send, _ := findNode(n, "review/send")
	assert.Equal(t, sdui.VariantPrimary, send.Props["variant"])
	r.Sent = 3
	header, _ = findNode(reviewView([]*review{r}, r.ID), "review/header")
	assert.Contains(t, texts(header), "5/5 · 3 comments sent")

	r.Filters.Reviewed = false
	n = reviewView([]*review{r}, r.ID)
	dir := findItem(t, n, "review/tree", "dir:pkg/sdui")
	assert.Equal(t, "3/3", dir.Detail)
	assert.Empty(t, dir.Children)
	assert.Contains(t, texts(n), "5 reviewed files hidden ·")
}

func TestReviewSwitcherWithSeveralReviews(t *testing.T) {
	a := grammarReview()
	b := newReview("ws", "/repo/.worktrees/feat", reviewKey{Worktree: "/repo/.worktrees/feat", Base: "main", Head: "feat"}, 2, &fakeReviewer{})
	b.Info = reviewInfo{Labels: reviewLabels{Left: "main", Right: "feat"}}
	n := reviewView([]*review{a, b}, b.ID)
	require.NoError(t, internalsdui.Validate(n))
	sw, ok := findNode(n, "review/switch")
	require.True(t, ok)
	assert.Equal(t, b.ID, sw.Props["value"])
	assert.Equal(t, []sdui.Option{{Value: a.ID, Label: "Working tree ↔ Before turn 3 · 1/5 · 0"}, {Value: b.ID, Label: "feat ↔ main · 0/0 · 0"}}, sw.Props["options"])
	assert.Equal(t, "empty", reviewView(nil, "").Type)
}

func texts(n sdui.Node) []string {
	var out []string
	if s, ok := n.Props["text"].(string); ok {
		out = append(out, s)
	}
	if s, ok := n.Props["label"].(string); ok && n.Type != "header" {
		out = append(out, s)
	}
	for _, c := range n.Children {
		out = append(out, texts(c)...)
	}
	return out
}
