package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestPageTargetsResolveRealLinks(t *testing.T) {
	src := "[up](../a.md) [same](b.md#x) [abs](/w/c/d.md) [out](/etc/e.md) [web](https://x.dev/f.md) [self](g.md) [png](h.png)\n\n```\n[code](i.md)\n```\n"

	assert.Equal(t, []string{"a.md", "sub/b.md", "c/d.md"}, pageTargets([]byte(src), "sub/g.md", "/w"))
}
