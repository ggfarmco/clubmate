package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"clubmate/pkg/contract"
	"clubmate/pkg/docext"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
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

const (
	maxDocCards        = 8
	docsOpenedTopic    = "docs.opened"
	docsPresentedTopic = "docs.presented"
	docsReadyTopic     = "docs.ready"
	codeUnknownCommand = -32004
)

type presentedDoc struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
}

type docsPresented struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Path      string `json:"path"`
	Title     string `json:"title"`
}

func (a *app) docPresented(p docsPresented) {
	s := a.session(p.Session)
	if s == nil || p.Path == "" || s.snapshot().Workspace != p.Workspace {
		return
	}
	s.mu.Lock()
	deleted := s.deleted
	if !deleted {
		s.recordLocked(actorClubmate, kindPresented, presentedDoc{Path: p.Path, Title: p.Title})
	}
	s.mu.Unlock()
	if !deleted {
		s.changed(changeUpdate, false)
	}
}

func (s *session) present(turn int, p presentedDoc) {
	p.Path = gitrepo.Canonical(p.Path)
	if s.presented == nil {
		s.presented = map[int][]presentedDoc{}
	}
	docs := s.presented[turn]
	if i := slices.IndexFunc(docs, func(d presentedDoc) bool { return d.Path == p.Path }); i >= 0 {
		docs[i].Title = cmp.Or(p.Title, docs[i].Title)
	} else {
		s.presented[turn] = append(docs, p)
	}
	if s.ended[turn] {
		s.linkDocs(turn)
	}
}

func (s *session) linkDocs(turn int) {
	type message struct {
		refs
		en *entry
	}
	var messages []message
	mentioned := map[string]bool{}
	for _, en := range s.entries {
		if en.Kind != entryAgent || en.Turn != turn {
			continue
		}
		s.findTasksLocked(en)
		r := docRefs(en.Text, s.rec.Worktree)
		for i, c := range r.cards {
			r.cards[i] = presentCard(c, s.presented[turn])
			mentioned[gitrepo.Canonical(c.Path)] = true
		}
		messages = append(messages, message{refs: r, en: en})
	}
	if len(messages) == 0 {
		return
	}
	root := s.rec.Worktree
	if root != "" {
		root = gitrepo.Canonical(root)
	}
	last := &messages[len(messages)-1]
	for _, p := range s.presented[turn] {
		if !mentioned[p.Path] {
			last.cards = append(last.cards, presentCard(docCard(p.Path, root), []presentedDoc{p}))
		}
	}
	for _, m := range messages {
		slices.SortStableFunc(m.cards, func(x, y sdui.DocCard) int {
			return cmp.Compare(boolInt(y.Presented), boolInt(x.Presented))
		})
		cards := m.cards[:min(len(m.cards), maxDocCards)]
		if len(cards) == 0 {
			cards = nil
		}
		if !maps.Equal(m.links, m.en.DocLinks) || !slices.Equal(cards, m.en.DocCards) || !slices.Equal(m.images, m.en.Images) || !maps.Equal(m.imageLinks, m.en.ImageLinks) {
			m.en.DocLinks, m.en.DocCards, m.en.Images, m.en.ImageLinks = m.links, cards, m.images, m.imageLinks
			s.touch(m.en)
		}
	}
}

func presentCard(c sdui.DocCard, presented []presentedDoc) sdui.DocCard {
	path := gitrepo.Canonical(c.Path)
	if i := slices.IndexFunc(presented, func(p presentedDoc) bool { return p.Path == path }); i >= 0 {
		c.Presented, c.Title = true, cmp.Or(presented[i].Title, c.Title)
	}
	return c
}

type refs struct {
	links      map[string]string
	cards      []sdui.DocCard
	images     []sdui.ImageRef
	imageLinks map[string]string
}

func docRefs(markdown, root string) refs {
	src := []byte(markdown)
	links, imageLinks := map[string]string{}, map[string]string{}
	var cards []sdui.DocCard
	var images []sdui.ImageRef
	add := func(target string) {
		if path, ok := resolveImage(target, root); ok {
			named := slices.ContainsFunc(images, func(r sdui.ImageRef) bool { return r.Path == path })
			if !named && len(images) < maxImages {
				images, named = append(images, imageRef(path, root)), true
			}
			if named {
				imageLinks[target] = path
			}
			return
		}
		path, ok := resolveDoc(target, root)
		if !ok {
			return
		}
		links[target] = path
		file, _, _ := strings.Cut(path, "#")
		if !slices.ContainsFunc(cards, func(c sdui.DocCard) bool { return c.Path == file }) {
			cards = append(cards, docCard(file, root))
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
	r := refs{cards: cards, images: images}
	if len(links) > 0 {
		r.links = links
	}
	if len(imageLinks) > 0 {
		r.imageLinks = imageLinks
	}
	return r
}

func docCard(path, root string) sdui.DocCard {
	return sdui.DocCard{Path: path, Kind: docext.Kind(path), Title: docext.Title(path), Rel: relTo(root, path)}
}

func relTo(root, path string) string {
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return r
		}
	}
	return path
}

func resolveDoc(target, root string) (string, bool) {
	path, anchor, ok := resolveTarget(target, root)
	if !ok || !docext.Is(path) && !docext.IsArtifact(path) || !isFile(path) {
		return "", false
	}
	if anchor != "" {
		path += "#" + anchor
	}
	return path, true
}

func resolveTarget(target, root string) (string, string, bool) {
	path, anchor, _ := strings.Cut(target, "#")
	if unescaped, err := url.PathUnescape(path); err == nil {
		path = unescaped
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", false
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		if root == "" {
			return "", "", false
		}
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), anchor, true
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

const (
	docsPlugin     = "docs"
	docGateOpen    = "open"
	docGatePresent = "present"
)

var (
	errDocsOnly   = errors.New("only the docs plugin calls this")
	errDocGateArg = errors.New("doc-gate needs a session, a path and mode open or present")
)

type gatedDoc struct {
	Workspace string `json:"workspace"`
	For       string `json:"for"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Title     string `json:"title,omitempty"`
}

type docGateArgs struct {
	Session string `json:"session"`
	For     string `json:"for"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Title   string `json:"title"`
}

type docGateResult struct {
	Queued bool `json:"queued"`
}

func (a *app) docGateCommand(ctx context.Context, cmd pluginsdk.CommandRun, args docGateArgs) (any, error) {
	if cmd.Via != docsPlugin || cmd.Agent {
		return nil, errDocsOnly
	}
	if args.Session == "" || args.Path == "" || args.Mode != docGateOpen && args.Mode != docGatePresent {
		return nil, errDocGateArg
	}
	s := a.session(args.Session)
	if s == nil {
		return docGateResult{}, nil
	}
	rec := s.snapshot()
	if rec.Workspace != cmd.WorkspaceID || !turnRunning(rec) {
		return docGateResult{}, nil
	}
	gates, err := a.gates(ctx)
	if err != nil {
		return nil, err
	}
	if !doneGated(gates, primaryOf(ctx, rec.Worktree), rec) {
		return docGateResult{}, nil
	}
	doc := gatedDoc{Workspace: rec.Workspace, For: cmp.Or(args.For, args.Session), Path: args.Path, Mode: args.Mode, Title: args.Title}
	s.mu.Lock()
	queued := turnRunning(s.rec)
	if queued {
		docs := slices.DeleteFunc(slices.Clone(s.rec.GatedDocs), func(d gatedDoc) bool { return d.For == doc.For && d.Path == doc.Path && d.Mode == doc.Mode })
		s.rec.GatedDocs = append(docs, doc)
	}
	s.mu.Unlock()
	if queued {
		s.changed(changeUpdate, true)
	}
	return docGateResult{Queued: queued}, nil
}

func turnRunning(rec record) bool {
	return !rec.closed() && (rec.Status == statusRunning || rec.Status == statusWaiting)
}

func (a *app) settleGatedDocs(s *session) {
	s.mu.Lock()
	docs, closed := s.rec.GatedDocs, s.rec.closed()
	s.replayAgain = s.replayingDocs
	if len(docs) == 0 || s.replayingDocs || turnRunning(s.rec) || !closed && s.rec.LimitWait != nil {
		s.mu.Unlock()
		return
	}
	if closed {
		s.rec.GatedDocs = nil
	}
	s.replayingDocs = !closed
	s.mu.Unlock()
	go func() {
		if closed {
			s.changed(changeUpdate, true)
			return
		}
		settled := a.replayDocs(docs)
		s.mu.Lock()
		again := s.replayAgain
		s.replayingDocs, s.replayAgain = false, false
		s.rec.GatedDocs = slices.DeleteFunc(slices.Clone(s.rec.GatedDocs), func(d gatedDoc) bool { return slices.Contains(settled, d) })
		s.mu.Unlock()
		if len(settled) > 0 {
			s.changed(changeUpdate, true)
		}
		if again {
			a.settleGatedDocs(s)
		}
	}()
}

func (a *app) replayDocs(docs []gatedDoc) []gatedDoc {
	var settled []gatedDoc
	for _, d := range docs {
		args := map[string]any{"path": d.Path, "session": d.For, "gated": false}
		id := "docs.open"
		if d.Mode == docGatePresent {
			id = "docs.present"
			if d.Title != "" {
				args["title"] = d.Title
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		err := a.host.RunCommand(ctx, id, d.Workspace, args, nil)
		cancel()
		if docsDown(err) {
			continue
		}
		logError("show a doc after its turn", err)
		settled = append(settled, d)
	}
	return settled
}

func docsDown(err error) bool {
	rpcErr, ok := errors.AsType[*jsonrpc2.Error](err)
	return ok && (rpcErr.Code == contract.CodePluginUnavailable || rpcErr.Code == codeUnknownCommand) || errors.Is(err, context.DeadlineExceeded)
}

func (a *app) settleAllGatedDocs() {
	for _, s := range a.allSessions() {
		a.settleGatedDocs(s)
	}
}
