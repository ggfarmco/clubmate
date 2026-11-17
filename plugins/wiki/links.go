package main

import (
	"cmp"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"clubmate/pkg/docext"
	"clubmate/plugins/internal/gitrepo"
)

var linkParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

type linkEntry struct {
	mod     time.Time
	size    int64
	targets []string
}

func pageTargets(src []byte, from, dir string) []string {
	var out []string
	_ = ast.Walk(linkParser.Parse(text.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		l, ok := n.(*ast.Link)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := linkTarget(string(l.Destination), from, dir); ok && t != from && !slices.Contains(out, t) {
			out = append(out, t)
		}
		return ast.WalkContinue, nil
	})
	return out
}

func linkTarget(dest, from, dir string) (string, bool) {
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	p := u.Path
	if filepath.IsAbs(p) {
		abs := gitrepo.Canonical(filepath.Clean(p))
		if !gitrepo.Within(abs, dir) {
			return "", false
		}
		rel, err := filepath.Rel(dir, abs)
		if err != nil {
			return "", false
		}
		p = filepath.ToSlash(rel)
	} else {
		p = path.Join(path.Dir(from), p)
	}
	page, err := cleanPage(p)
	return page, err == nil
}

func (w *wiki) linkedFrom(s *Store, pages []Page, target string) []Page {
	dir := gitrepo.Canonical(s.p.Dir)
	w.mu.Lock()
	prev := w.links[s.p.Dir]
	w.mu.Unlock()
	next := make(map[string]linkEntry, len(pages))
	var out []Page
	for _, pg := range pages {
		if docext.Kind(pg.Path) != docext.KindMarkdown {
			continue
		}
		info, err := os.Stat(filepath.Join(s.p.Dir, filepath.FromSlash(pg.Path)))
		if err != nil {
			continue
		}
		e, ok := prev[pg.Path]
		if !ok || !e.mod.Equal(info.ModTime()) || e.size != info.Size() {
			src, err := s.readFile(pg.Path)
			if err != nil {
				continue
			}
			e = linkEntry{mod: info.ModTime(), size: info.Size(), targets: pageTargets(src, pg.Path, dir)}
		}
		next[pg.Path] = e
		if slices.Contains(e.targets, target) {
			out = append(out, pg)
		}
	}
	w.mu.Lock()
	w.links[s.p.Dir] = next
	w.mu.Unlock()
	slices.SortStableFunc(out, func(a, b Page) int {
		return cmp.Compare(boolInt(a.Shadow), boolInt(b.Shadow))
	})
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
