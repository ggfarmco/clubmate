package main

import (
	"cmp"
	"context"
	"html"
	"regexp"
	"slices"
	"strings"

	"clubmate/pkg/docext"
)

const snippetRunes = 160

var (
	hiddenBlock = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)\s*>`)
	htmlTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRun    = regexp.MustCompile(`[ \t]+`)
)

type doc struct {
	Page  string
	Title string
	Body  string
}

type Hit struct {
	Page    string
	Title   string
	Snippet string
}

type scored struct {
	hit   Hit
	score int
}

func (s *Store) Search(ctx context.Context, query string, shadow bool, limit int) ([]Hit, error) {
	pages, err := s.List(ctx, shadow)
	if err != nil {
		return nil, err
	}
	return s.searchIn(pages, query, shadow, limit), nil
}

func (s *Store) searchIn(pages []Page, query string, shadow bool, limit int) []Hit {
	docs := make([]doc, 0, len(pages))
	for _, p := range pages {
		if p.Shadow && !shadow {
			continue
		}
		body, err := s.readFile(p.Path)
		if err != nil {
			continue
		}
		docs = append(docs, doc{Page: p.Path, Title: p.Title, Body: string(body)})
	}
	return search(docs, query, limit)
}

func search(docs []doc, query string, limit int) []Hit {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var found []scored
	for _, d := range docs {
		body := d.Body
		if docext.IsArtifact(d.Page) {
			body = visibleText(body)
		}
		if score, snippet, ok := match(d.Title, body, terms); ok {
			found = append(found, scored{hit: Hit{Page: d.Page, Title: d.Title, Snippet: snippet}, score: score})
		}
	}
	slices.SortFunc(found, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.hit.Page, b.hit.Page))
	})
	out := make([]Hit, 0, min(limit, len(found)))
	for _, f := range found[:min(limit, len(found))] {
		out = append(out, f.hit)
	}
	return out
}

func match(title, body string, terms []string) (int, string, bool) {
	title = strings.ToLower(title)
	lines := strings.Split(body, "\n")
	lower := make([]string, len(lines))
	for i, l := range lines {
		lower[i] = strings.ToLower(l)
	}
	score, snippet := 0, ""
	for _, term := range terms {
		best := 0
		if strings.Contains(title, term) {
			best = 3
		}
		for i, l := range lower {
			if !strings.Contains(l, term) {
				continue
			}
			if snippet == "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				snippet = lines[i]
			}
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				best = max(best, 2)
			} else {
				best = max(best, 1)
			}
		}
		if best == 0 {
			return 0, "", false
		}
		score += best
	}
	if snippet == "" {
		for i, l := range lower {
			if strings.Contains(l, terms[0]) {
				snippet = lines[i]
				break
			}
		}
	}
	return score, trimSnippet(snippet), true
}

func trimSnippet(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > snippetRunes {
		return string(r[:snippetRunes])
	}
	return s
}

func visibleText(src string) string {
	s := hiddenBlock.ReplaceAllString(src, "\n")
	s = htmlTag.ReplaceAllStringFunc(s, func(tag string) string {
		if blockTag(tag) {
			return "\n"
		}
		return ""
	})
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(blankRun.ReplaceAllString(l, " "))
	}
	return strings.Join(lines, "\n")
}

func blockTag(tag string) bool {
	name := strings.ToLower(strings.TrimLeft(tag, "</"))
	for _, b := range []string{"p", "div", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "section", "article", "pre", "table", "ul", "ol"} {
		if name == b || strings.HasPrefix(name, b+" ") || strings.HasPrefix(name, b+">") || strings.HasPrefix(name, b+"/") {
			return true
		}
	}
	return false
}
