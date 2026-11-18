package main

import (
	"html"
	"maps"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	historyAnchor = "wiki:history"
	marginChanges = 3
	warnIcon      = `<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2 14.5 13.5h-13Z"/><path d="M8 6.5v3M8 11.6v.1"/></svg>`
	folderIcon    = `<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h3l1.5 1.5h4.5A1.5 1.5 0 0 1 14 6v5.5a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 11.5Z"/></svg>`
	noteIcon      = `<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="7" width="9" height="7" rx="1.5"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/></svg>`
)

var headingTag = regexp.MustCompile(`(?s)<h([1-3]) id="([^"]*)"[^>]*>(.*?)</h[1-3]>`)

type heading struct {
	Level int
	ID    string
	Text  string
}

type linkedPage struct {
	Title  string
	Path   string
	Shadow bool
}

type folderEntry struct {
	Title   string
	Path    string
	Updated time.Time
	Folder  bool
}

type marginData struct {
	Path    string
	Page    Page
	Outline []heading
	Changes []Change
	Total   int
	Linked  []linkedPage
	Folder  []folderEntry
	Now     time.Time
}

func indexFolder(page string, pages []Page) (string, bool) {
	folder := path.Dir(page)
	if folder == "." {
		return "", false
	}
	idx, ok := indexPage(folder, pages)
	return folder, ok && idx.Path == page
}

func folderEntries(root, folder, self string, pages []Page) []folderEntry {
	under := slices.DeleteFunc(slices.Clone(pages), func(pg Page) bool { return !strings.HasPrefix(pg.Path, folder+"/") })
	files, byDir := group(folder+"/", under)
	var out []folderEntry
	for _, name := range slices.Sorted(maps.Keys(byDir)) {
		inside := byDir[name]
		e := folderEntry{Title: name, Folder: true}
		if idx, ok := indexPage(folder+"/"+name, inside); ok {
			e.Title, e.Path = idx.Title, filepath.Join(root, filepath.FromSlash(idx.Path))
		}
		for _, pg := range inside {
			e.Updated = maxTime(e.Updated, pg.Updated)
		}
		out = append(out, e)
	}
	for _, pg := range files {
		if pg.Path != self {
			out = append(out, folderEntry{Title: pg.Title, Path: filepath.Join(root, filepath.FromSlash(pg.Path)), Updated: pg.Updated})
		}
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func headings(body string) []heading {
	var out []heading
	for _, m := range headingTag.FindAllStringSubmatch(body, -1) {
		level, _ := strconv.Atoi(m[1])
		text := strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(m[3], "")))
		if text != "" {
			out = append(out, heading{Level: level, ID: html.UnescapeString(m[2]), Text: text})
		}
	}
	return out
}

func docHref(abs, anchor string) string {
	href := "clubmate-doc:" + (&url.URL{Path: abs}).EscapedPath()
	if anchor != "" {
		href += "#" + anchor
	}
	return href
}

func margin(d marginData) string {
	var sections, folded []string
	if len(d.Folder) > 0 {
		var b strings.Builder
		b.WriteString(`<div class="wm-sec"><div class="wm-cap">In this folder · ` + strconv.Itoa(len(d.Folder)) + `</div>`)
		for _, e := range d.Folder {
			icon := ""
			if e.Folder {
				icon = folderIcon
			}
			row := icon + `<span class="wm-label">` + html.EscapeString(e.Title) + `</span><span class="wm-detail">` + shortTime(e.Updated, d.Now) + `</span>`
			if e.Path == "" {
				b.WriteString(`<div class="wm-row" title="` + html.EscapeString(e.Title) + `">` + row + `</div>`)
				continue
			}
			b.WriteString(`<a class="wm-row" href="` + html.EscapeString(docHref(e.Path, "")) + `" title="` + html.EscapeString(e.Title) + `">` + row + `</a>`)
		}
		b.WriteString(`</div>`)
		sections = append(sections, b.String())
		folded = append(folded, strconv.Itoa(len(d.Folder))+" in this folder")
	}
	if len(d.Outline) >= 2 {
		var b strings.Builder
		b.WriteString(`<nav class="wm-sec"><div class="wm-cap">On this page</div>`)
		for _, h := range d.Outline {
			b.WriteString(`<a class="wm-oln wm-l` + strconv.Itoa(h.Level) + `" href="#` + html.EscapeString(h.ID) + `" title="` + html.EscapeString(h.Text) + `">` + html.EscapeString(h.Text) + `</a>`)
		}
		b.WriteString(`</nav>`)
		sections = append(sections, b.String())
		folded = append(folded, "On this page")
	}
	if d.Page.Stale && d.Page.StaleSource != "" {
		more := ""
		if d.Page.StaleMore > 0 {
			more = " and " + plural(d.Page.StaleMore, "more path")
		}
		sections = append(sections, `<div class="wm-sec wm-stale"><div class="wm-cap wm-warn">`+warnIcon+`Stale</div><div class="wm-text"><code>`+html.EscapeString(d.Page.StaleSource)+`</code>`+more+` changed `+staleWhen(d.Page.StaleAt, d.Now)+`, after this page.</div></div>`)
	}
	if len(d.Changes) > 0 {
		var b strings.Builder
		b.WriteString(`<div class="wm-sec"><div class="wm-cap">Changes</div>`)
		for _, c := range d.Changes[:min(len(d.Changes), marginChanges)] {
			b.WriteString(`<div class="wm-row" title="` + html.EscapeString(c.Summary+" · "+c.By+" · "+c.At.Local().Format(timeLayout)) + `"><span class="wm-label">` + html.EscapeString(c.Summary) + `</span><span class="wm-detail">` + shortTime(c.At, d.Now) + `</span></div>`)
		}
		if d.Total > marginChanges {
			b.WriteString(`<a class="wm-row wm-all" href="` + html.EscapeString(docHref(d.Path, historyAnchor)) + `"><span class="wm-label">All ` + plural(d.Total, "change") + `</span><kbd>⌘Y</kbd></a>`)
		}
		b.WriteString(`</div>`)
		sections = append(sections, b.String())
		folded = append(folded, plural(d.Total, "change"))
	}
	if d.Page.Stale {
		folded = append(folded, staleBadge)
	}
	if len(d.Linked) > 0 {
		var b strings.Builder
		b.WriteString(`<div class="wm-sec"><div class="wm-cap">Linked from</div>`)
		for _, l := range d.Linked {
			icon := ""
			if l.Shadow {
				icon = noteIcon
			}
			b.WriteString(`<a class="wm-row" href="` + html.EscapeString(docHref(l.Path, "")) + `" title="` + html.EscapeString(l.Title) + `">` + icon + `<span class="wm-label">` + html.EscapeString(l.Title) + `</span></a>`)
		}
		b.WriteString(`</div>`)
		sections = append(sections, b.String())
		folded = append(folded, plural(len(d.Linked), "link"))
	}
	if len(sections) == 0 {
		return ""
	}
	body := strings.Join(sections, "")
	return `<aside class="wiki-margin" aria-label="About this page">` + body + `</aside><details class="wiki-fold"><summary>` + html.EscapeString(strings.Join(folded, " · ")) + `</summary><div class="wm-fold">` + body + `</div></details>`
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func staleWhen(at, now time.Time) string {
	if sameDay(at, now) {
		return "today " + at.Local().Format("15:04")
	}
	return at.Local().Format(timeLayout)
}
