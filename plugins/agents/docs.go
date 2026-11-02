package main

import (
	"bytes"
	"cmp"
	"context"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"clubmate/pkg/docext"
	"clubmate/pkg/pluginsdk"
)

type docsArgs struct {
	Session string `json:"session"`
}

type docRef struct {
	Path    string    `json:"path"`
	Session string    `json:"session"`
	At      time.Time `json:"at"`
}

type docsResult struct {
	Docs []docRef `json:"docs"`
}

func (a *app) docsCommand(_ context.Context, cmd pluginsdk.CommandRun, args docsArgs) (any, error) {
	var sessions []*session
	if args.Session != "" {
		s, err := a.lookup(cmd.WorkspaceID, args.Session)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	} else {
		for _, rec := range a.list(cmd.WorkspaceID) {
			if s := a.session(rec.ID); s != nil {
				sessions = append(sessions, s)
			}
		}
	}
	latest := map[string]docRef{}
	for _, s := range sessions {
		for _, d := range s.docs() {
			if cur, ok := latest[d.Path]; !ok || d.At.After(cur.At) {
				latest[d.Path] = d
			}
		}
	}
	docs := slices.SortedFunc(maps.Values(latest), func(x, y docRef) int {
		return cmp.Or(y.At.Compare(x.At), cmp.Compare(x.Path, y.Path))
	})
	return docsResult{Docs: append([]docRef{}, docs...)}, nil
}

func (s *session) docs() []docRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	var out []docRef
	for _, en := range s.entries {
		if en.Doc != "" {
			out = append(out, docRef{Path: en.Doc, Session: s.rec.ID, At: en.At})
		}
	}
	for _, sub := range s.subagents {
		for _, en := range sub.entries {
			if path := s.docOf(en.Tool); path != "" {
				out = append(out, docRef{Path: path, Session: s.rec.ID, At: en.At})
			}
		}
	}
	return out
}

func (s *session) linkDocs(turn int) {
	for _, en := range s.entries {
		if en.Kind != entryAgent || en.Turn != turn {
			continue
		}
		if links := docLinks(en.Text, s.rec.Worktree); !maps.Equal(links, en.DocLinks) {
			en.DocLinks, en.node = links, nil
		}
	}
}

func docLinks(markdown, root string) map[string]string {
	src := []byte(markdown)
	links := map[string]string{}
	add := func(target string) {
		if path, ok := resolveDoc(target, root); ok {
			links[target] = path
		}
	}
	_ = ast.Walk(goldmark.DefaultParser().Parse(text.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			if u, err := url.Parse(string(n.Destination)); err == nil && u.Scheme == "" {
				add(string(n.Destination))
			}
		case *ast.CodeSpan:
			var code bytes.Buffer
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				switch c := c.(type) {
				case *ast.Text:
					code.Write(c.Value(src))
				case *ast.String:
					code.Write(c.Value)
				}
			}
			if !bytes.ContainsAny(code.Bytes(), " \t\n") {
				add(code.String())
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if len(links) == 0 {
		return nil
	}
	return links
}

func resolveDoc(target, root string) (string, bool) {
	path, anchor, _ := strings.Cut(target, "#")
	if unescaped, err := url.PathUnescape(path); err == nil {
		path = unescaped
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		if root == "" {
			return "", false
		}
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !docext.Is(path) && !docext.IsArtifact(path) {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	if anchor != "" {
		path += "#" + anchor
	}
	return path, true
}
