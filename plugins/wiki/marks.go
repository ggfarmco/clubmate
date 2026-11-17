package main

import (
	"html"
	"regexp"
	"strings"
)

const matchAnchor = "wiki-match-"

var (
	anyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	skipOpen   = regexp.MustCompile(`(?i)^<(script|style)\b|^<pre\b[^>]*\bclass="mermaid"`)
	skipCloses = map[string]string{"script": "</script", "style": "</style", "pre": "</pre"}
)

func termsPattern(query string) *regexp.Regexp {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return nil
	}
	for i, t := range terms {
		terms[i] = regexp.QuoteMeta(t)
	}
	return regexp.MustCompile("(?i)" + strings.Join(terms, "|"))
}

func markMatches(body, query, id string) (string, bool) {
	re := termsPattern(query)
	if re == nil {
		return body, false
	}
	var b strings.Builder
	first := true
	skip := ""
	last := 0
	text := func(seg string) {
		if skip != "" || !re.MatchString(html.UnescapeString(seg)) {
			b.WriteString(seg)
			return
		}
		plain := html.UnescapeString(seg)
		prev := 0
		for _, m := range re.FindAllStringIndex(plain, -1) {
			b.WriteString(html.EscapeString(plain[prev:m[0]]))
			if first {
				b.WriteString(`<mark id="` + id + `">`)
				first = false
			} else {
				b.WriteString("<mark>")
			}
			b.WriteString(html.EscapeString(plain[m[0]:m[1]]) + "</mark>")
			prev = m[1]
		}
		b.WriteString(html.EscapeString(plain[prev:]))
	}
	for _, loc := range anyTag.FindAllStringIndex(body, -1) {
		text(body[last:loc[0]])
		tag := body[loc[0]:loc[1]]
		b.WriteString(tag)
		last = loc[1]
		lower := strings.ToLower(tag)
		switch {
		case skip != "" && strings.HasPrefix(lower, skipCloses[skip]):
			skip = ""
		case skip == "":
			if m := skipOpen.FindStringSubmatch(lower); m != nil {
				skip = strings.ToLower(m[1])
				if skip == "" {
					skip = "pre"
				}
			}
		}
	}
	text(body[last:])
	return b.String(), !first
}
