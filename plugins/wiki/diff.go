package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

const maxDiffLines = 2000

var (
	hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)
	mdHeading  = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
)

type hunk struct {
	context string
	at      int
	lines   strings.Builder
}

func diffHTML(c Change, when, diff string, after []string) string {
	var b strings.Builder
	b.WriteString(`<div class="diff-head"><p class="diff-title">` + html.EscapeString(c.Summary) + `</p><p class="diff-meta">` + html.EscapeString(c.By+" · "+when) + ` · ` + counts(c) + `</p></div>`)
	var hunks []*hunk
	shown, hidden, oldLn, newLn := 0, 0, 0, 0
	for line := range strings.SplitSeq(strings.TrimRight(diff, "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			oldLn, _ = strconv.Atoi(m[1])
			newLn, _ = strconv.Atoi(m[2])
			hunks = append(hunks, &hunk{context: strings.TrimSpace(m[3])})
			continue
		}
		if len(hunks) == 0 || line == "" || line[0] == '\\' {
			continue
		}
		h := hunks[len(hunks)-1]
		kind, ln := "", 0
		switch line[0] {
		case '+':
			kind, ln = "add", newLn
			newLn++
		case '-':
			kind, ln = "del", oldLn
			oldLn++
		case ' ':
			ln = newLn
			oldLn++
			newLn++
		default:
			continue
		}
		if kind != "" && h.at == 0 {
			h.at = max(newLn-1, 1)
		}
		if shown >= maxDiffLines {
			hidden++
			continue
		}
		shown++
		sign := strings.Replace(line[:1], "-", "−", 1)
		h.lines.WriteString(`<span class="line ` + kind + `"><span class="ln">` + strconv.Itoa(ln) + `</span><span class="sign">` + sign + `</span>` + html.EscapeString(line[1:]) + "\n</span>")
	}
	for _, h := range hunks {
		if h.lines.Len() == 0 {
			continue
		}
		if at := hunkHeading(h.context, after, h.at); at != "" {
			b.WriteString(`<p class="diff-at">` + html.EscapeString(at) + `</p>`)
		}
		b.WriteString(`<pre class="diff"><code>` + h.lines.String() + `</code></pre>`)
	}
	if shown == 0 {
		b.WriteString(`<p class="diff-at">No text changes</p>`)
	}
	if hidden > 0 {
		b.WriteString(`<p class="diff-at">` + plural(hidden, "more line") + ` not shown</p>`)
	}
	return b.String()
}

func counts(c Change) string {
	return `<span class="diff-add">+` + strconv.Itoa(c.Added) + `</span> <span class="diff-del">−` + strconv.Itoa(c.Removed) + `</span>`
}

func hunkHeading(context string, after []string, start int) string {
	for i := min(start, len(after)) - 1; i >= 0; i-- {
		if m := mdHeading.FindStringSubmatch(after[i]); m != nil {
			return m[1]
		}
	}
	if m := mdHeading.FindStringSubmatch(context); m != nil {
		return m[1]
	}
	return ""
}
