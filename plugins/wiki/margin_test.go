package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarginOmitsEmptySections(t *testing.T) {
	d := marginData{Path: "/w/a.md", Outline: []heading{{Level: 2, ID: "one", Text: "One"}}}
	assert.Empty(t, margin(d))

	d.Changes, d.Total = []Change{{Summary: "created", At: time.Now()}}, 1
	got := margin(d)
	assert.Contains(t, got, `<aside class="wiki-margin"`)
	assert.Contains(t, got, "<summary>1 change</summary>")
	assert.NotContains(t, got, "On this page")
	assert.NotContains(t, got, "Linked from")
	assert.NotContains(t, got, "Stale")
	assert.NotContains(t, got, "All 1 change")
}

func TestMarginEscapesPageStrings(t *testing.T) {
	d := marginData{
		Path:    `/w/"a b".md`,
		Page:    Page{Stale: true, StaleSource: "<img>.go", StaleAt: time.Now()},
		Outline: []heading{{Level: 2, ID: `x"y`, Text: "<b>1</b>"}, {Level: 3, ID: "z", Text: "Z"}},
		Changes: []Change{{Summary: "<script>alert(1)</script>", By: `"me"`}},
		Total:   5,
		Linked:  []linkedPage{{Title: `<i>L</i>`, Path: "/w/l.md"}},
	}

	got := margin(d)
	for _, raw := range []string{"<script>", "<img>", "<b>1", "<i>L", `x"y`, `"me"`} {
		assert.NotContains(t, got, raw)
	}
	assert.Contains(t, got, `href="clubmate-doc:/w/%22a%20b%22.md#wiki:history"`)
	assert.Contains(t, got, "<summary>On this page · 5 changes · stale · 1 link</summary>")
}

func TestIndexPageMarginListsItsFolderFirst(t *testing.T) {
	day := time.Date(2026, 11, 16, 10, 0, 0, 0, time.Local)
	pages := []Page{
		{Path: "guides/index.md", Title: "Guides"},
		{Path: "guides/a.md", Title: "<b>A</b>", Updated: day},
		{Path: "guides/plain/y.md", Title: "Y", Updated: day},
		{Path: "guides/sub/index.md", Title: "Sub", Updated: day},
		{Path: "guides/sub/x.md", Title: "X", Updated: day.Add(time.Hour)},
		{Path: "guidesx.md", Title: "Other"},
	}
	folder, ok := indexFolder("guides/index.md", pages)
	require.True(t, ok)
	_, ok = indexFolder("guides/a.md", pages)
	assert.False(t, ok)

	here := folderEntries("/w", folder, "guides/index.md", pages)
	assert.Equal(t, []folderEntry{
		{Title: "plain", Updated: day, Folder: true},
		{Title: "Sub", Path: "/w/guides/sub/index.md", Updated: day.Add(time.Hour), Folder: true},
		{Title: "<b>A</b>", Path: "/w/guides/a.md", Updated: day},
	}, here)
	got := margin(marginData{Path: "/w/guides/index.md", Folder: here, Changes: []Change{{Summary: "x", At: day}}, Total: 1, Now: day})
	assert.Contains(t, got, "In this folder · 3")
	assert.NotContains(t, got, "<b>A")
	assert.Contains(t, got, "<summary>3 in this folder · 1 change</summary>")
}

func TestPageTargetsResolveRealLinks(t *testing.T) {
	src := "[up](../a.md) [same](b.md#x) [abs](/w/c/d.md) [out](/etc/e.md) [web](https://x.dev/f.md) [self](g.md) [png](h.png)\n\n```\n[code](i.md)\n```\n"

	assert.Equal(t, []string{"a.md", "sub/b.md", "c/d.md"}, pageTargets([]byte(src), "sub/g.md", "/w"))
}
