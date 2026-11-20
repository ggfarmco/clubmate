package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/docext"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/wikiroot"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	historyLimit = 50
)

type searchArgs struct {
	Query  string `json:"query"`
	Shadow bool   `json:"shadow"`
	Limit  int    `json:"limit"`
}

type listArgs struct {
	Shadow bool `json:"shadow"`
}

type pageArgs struct {
	Page string `json:"page"`
}

type locateArgs struct {
	Path string `json:"path"`
	Home bool   `json:"home"`
}

type locateOut struct {
	ID      string `json:"id,omitempty"`
	Path    string `json:"path,omitempty"`
	Title   string `json:"title,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
	History bool   `json:"history,omitempty"`
}

type writeArgs struct {
	Page    string   `json:"page"`
	Content string   `json:"content"`
	Summary string   `json:"summary"`
	Sources []string `json:"sources"`
}

type hitOut struct {
	Page    string `json:"page"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

type pageOut struct {
	Page    string    `json:"page"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
	By      string    `json:"by"`
	Stale   bool      `json:"stale"`
}

type readOut struct {
	Page    string    `json:"page"`
	Title   string    `json:"title"`
	Content string    `json:"content"`
	Sources []string  `json:"sources"`
	Updated time.Time `json:"updated"`
	By      string    `json:"by"`
	Stale   bool      `json:"stale"`
	Path    string    `json:"path"`
}

type writeOut struct {
	Page string `json:"page"`
	Path string `json:"path"`
}

type changeOut struct {
	At      time.Time `json:"at"`
	By      string    `json:"by"`
	Summary string    `json:"summary"`
}

type caller struct {
	folder string
	by     string
}

func (w *wiki) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "wiki.search":
		var args searchArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.searchCmd(ctx, cmd, args)
	case "wiki.list":
		var args listArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.listCmd(ctx, cmd, args)
	case "wiki.read":
		var args pageArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.readCmd(ctx, cmd, args)
	case "wiki.locate":
		var args locateArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.locateCmd(ctx, cmd, args)
	case "wiki.write":
		var args writeArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.writeCmd(ctx, cmd, args)
	case "wiki.delete":
		var args pageArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return struct{}{}, w.deleteCmd(ctx, cmd, args)
	case "wiki.history":
		var args pageArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return w.historyCmd(ctx, cmd, args)
	}
	return nil, fmt.Errorf("unknown command %q", cmd.ID)
}

func (w *wiki) who(ctx context.Context, cmd pluginsdk.CommandRun) (caller, error) {
	ws, err := w.h.GetWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return caller{}, err
	}
	if ws.Home || len(ws.Folders) == 0 {
		return caller{}, errNoWiki
	}
	c := caller{by: userName, folder: ws.Folders[0].Root()}
	for _, f := range ws.Folders {
		if _, ok := gitrepo.RepoRoot(gitrepo.Canonical(f.Root())); ok {
			c.folder = f.Root()
			break
		}
	}
	if cmd.Caller == "" {
		return c, nil
	}
	sessions, err := agentcall.Sessions(ctx, w.h, ws.ID)
	if err != nil {
		logError("sessions "+ws.ID, err)
		return c, nil
	}
	if s, ok := agentcall.Find(sessions, cmd.Caller); ok {
		c.by = cmp.Or(s.Title, s.ID)
		if s.Worktree != "" {
			c.folder = s.Worktree
		}
	}
	return c, nil
}

func (w *wiki) found(ctx context.Context, cmd pluginsdk.CommandRun) (*Store, bool, error) {
	c, err := w.who(ctx, cmd)
	if err != nil {
		return nil, false, err
	}
	p, ok, err := wikiroot.Find(ctx, c.folder)
	if err != nil || !ok {
		return nil, false, err
	}
	return Open(p, w.now), true, nil
}

func (w *wiki) searchCmd(ctx context.Context, cmd pluginsdk.CommandRun, args searchArgs) (any, error) {
	out := struct {
		Hits []hitOut `json:"hits"`
	}{Hits: []hitOut{}}
	s, ok, err := w.found(ctx, cmd)
	if err != nil || !ok {
		return out, err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	hits, err := s.Search(ctx, args.Query, args.Shadow, min(limit, maxLimit))
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		out.Hits = append(out.Hits, hitOut(h))
	}
	return out, nil
}

func (w *wiki) listCmd(ctx context.Context, cmd pluginsdk.CommandRun, args listArgs) (any, error) {
	out := struct {
		Pages []pageOut `json:"pages"`
	}{Pages: []pageOut{}}
	s, ok, err := w.found(ctx, cmd)
	if err != nil || !ok {
		return out, err
	}
	pages, err := s.List(ctx, args.Shadow)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		out.Pages = append(out.Pages, pageOut{Page: p.Path, Title: p.Title, Updated: p.Updated, By: p.By, Stale: p.Stale})
	}
	return out, nil
}

func (w *wiki) readCmd(ctx context.Context, cmd pluginsdk.CommandRun, args pageArgs) (any, error) {
	s, ok, err := w.found(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if !ok {
		if _, err := cleanPage(args.Page); err != nil {
			return nil, err
		}
		return nil, errNotFound
	}
	p, content, err := s.Read(ctx, args.Page)
	if err != nil {
		return nil, err
	}
	sources := p.Sources
	if sources == nil {
		sources = []string{}
	}
	return readOut{Page: p.Path, Title: p.Title, Content: content, Sources: sources, Updated: p.Updated, By: p.By, Stale: p.Stale, Path: filepath.Join(s.p.Dir, filepath.FromSlash(p.Path))}, nil
}

func (w *wiki) locateCmd(ctx context.Context, cmd pluginsdk.CommandRun, args locateArgs) (any, error) {
	target, anchor, _ := strings.Cut(args.Path, "#")
	ws, err := w.h.GetWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	projects := w.projects(ctx, ws)
	if args.Home {
		return home(ctx, projects, w.now)
	}
	abs := gitrepo.Canonical(filepath.Clean(target))
	if !filepath.IsAbs(target) || !scope(ws, projects)(abs) {
		return nil, errNotInWorkspace
	}
	p, page, ok := locate(projects, abs)
	if !ok {
		return locateOut{}, nil
	}
	out := locateOut{ID: pageID(p.Name, page), Path: filepath.Join(p.Dir, filepath.FromSlash(page)), Title: docext.FileTitle(page), Anchor: anchor, History: anchor == historyAnchor}
	found, _, err := Open(p, w.now).Read(ctx, page)
	switch {
	case errors.Is(err, errNotFound), errors.Is(err, errNotPage):
		return locateOut{}, nil
	case errors.Is(err, errTooLarge):
		return out, nil
	case err != nil:
		return nil, err
	}
	out.Title = found.Title
	return out, nil
}

func home(ctx context.Context, projects []wikiroot.Project, now func() time.Time) (locateOut, error) {
	for _, p := range projects {
		pages, err := Open(p, now).List(ctx, false)
		if err != nil {
			return locateOut{}, err
		}
		pg, ok := homePage(pages)
		if !ok {
			continue
		}
		return locateOut{ID: pageID(p.Name, pg.Path), Path: filepath.Join(p.Dir, filepath.FromSlash(pg.Path)), Title: pg.Title}, nil
	}
	return locateOut{}, nil
}

func homePage(pages []Page) (Page, bool) {
	for _, name := range indexNames {
		if i := slices.IndexFunc(pages, func(pg Page) bool { return pg.Path == name }); i >= 0 {
			return pages[i], true
		}
	}
	id, ok := firstPage(folderItems("", "", pages, nil, false, true))
	if !ok {
		return Page{}, false
	}
	i := slices.IndexFunc(pages, func(pg Page) bool { return pageID("", pg.Path) == id })
	return pages[i], i >= 0
}

func firstPage(items []sdui.TreeItem) (string, bool) {
	for _, it := range items {
		switch {
		case it.MirrorOf != "":
			return it.MirrorOf, true
		case !strings.HasPrefix(it.ID, dirPrefix):
			return it.ID, true
		}
		if id, ok := firstPage(it.Children); ok {
			return id, true
		}
	}
	return "", false
}

func (w *wiki) writeCmd(ctx context.Context, cmd pluginsdk.CommandRun, args writeArgs) (any, error) {
	c, err := w.who(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if _, err := cleanPage(args.Page); err != nil {
		return nil, err
	}
	p, err := wikiroot.Ensure(ctx, c.folder)
	if err != nil {
		return nil, err
	}
	s := Open(p, w.now)
	page, err := s.Write(ctx, WriteReq{Page: args.Page, Content: args.Content, Summary: args.Summary, By: c.by, Worktree: c.folder, Sources: args.Sources})
	if err != nil {
		return nil, err
	}
	w.changed(ctx, cmd.WorkspaceID, page.Path)
	w.renderAll(ctx)
	return writeOut{Page: page.Path, Path: filepath.Join(p.Dir, filepath.FromSlash(page.Path))}, nil
}

func (w *wiki) deleteCmd(ctx context.Context, cmd pluginsdk.CommandRun, args pageArgs) error {
	c, err := w.who(ctx, cmd)
	if err != nil {
		return err
	}
	page, err := cleanPage(args.Page)
	if err != nil {
		return err
	}
	p, ok, err := wikiroot.Find(ctx, c.folder)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	if err := Open(p, w.now).Delete(ctx, page, c.by); err != nil {
		return err
	}
	w.changed(ctx, cmd.WorkspaceID, page)
	w.renderAll(ctx)
	return nil
}

func (w *wiki) historyCmd(ctx context.Context, cmd pluginsdk.CommandRun, args pageArgs) (any, error) {
	out := struct {
		Changes []changeOut `json:"changes"`
	}{Changes: []changeOut{}}
	s, ok, err := w.found(ctx, cmd)
	if err != nil || !ok {
		return out, err
	}
	changes, err := s.History(ctx, args.Page, historyLimit)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		out.Changes = append(out.Changes, changeOut{At: c.At, By: c.By, Summary: c.Summary})
	}
	return out, nil
}
