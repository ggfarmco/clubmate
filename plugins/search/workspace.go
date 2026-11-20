package main

import (
	"cmp"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/wikiroot"
)

const (
	groupSessions     = "sessions"
	groupTasks        = "tasks"
	groupWiki         = "wiki"
	groupDocs         = "docs"
	defaultGroupLimit = 3
	wikiHitLimit      = 100
	lineSep           = " · "
)

type workspaceArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Group string `json:"group"`
}

type searchItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Line   string `json:"line"`
	Target string `json:"target"`
}

type searchGroup struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Total int          `json:"total"`
	Items []searchItem `json:"items"`
}

type sourceError struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type workspaceResult struct {
	Groups []searchGroup `json:"groups"`
	Errors []sourceError `json:"errors,omitempty"`
}

type source struct {
	id    string
	title string
	find  func(ctx context.Context, h *pluginsdk.Host, workspaceID, query string) ([]searchItem, error)
}

type found struct {
	items []searchItem
	err   error
}

var sources = []source{
	{id: groupSessions, title: "Sessions", find: findSessions},
	{id: groupTasks, title: "Tasks", find: findTasks},
	{id: groupWiki, title: "Wiki pages", find: findWikiPages},
	{id: groupDocs, title: "Docs", find: findDocs},
}

func (a *app) searchWorkspace(ctx context.Context, h *pluginsdk.Host, workspaceID string, args workspaceArgs) (workspaceResult, error) {
	picked := sources
	if args.Group != "" {
		i := indexOfSource(args.Group)
		if i < 0 {
			return workspaceResult{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "unknown group " + strconv.Quote(args.Group)}
		}
		picked = sources[i : i+1]
	}
	res := workspaceResult{Groups: []searchGroup{}}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return res, nil
	}
	results := make([]found, len(picked))
	var wg sync.WaitGroup
	for i, s := range picked {
		wg.Go(func() {
			items, err := s.find(ctx, h, workspaceID, query)
			results[i] = found{items: items, err: err}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return workspaceResult{}, err
	}
	limit := cmp.Or(max(args.Limit, 0), defaultGroupLimit)
	for i, s := range picked {
		r := results[i]
		if r.err != nil {
			res.Errors = append(res.Errors, sourceError{ID: s.id, Error: r.err.Error()})
			continue
		}
		if len(r.items) == 0 {
			continue
		}
		items := r.items
		if args.Group == "" {
			items = items[:min(len(items), limit)]
		}
		res.Groups = append(res.Groups, searchGroup{ID: s.id, Title: s.title, Total: len(r.items), Items: items})
	}
	return res, nil
}

func indexOfSource(id string) int {
	for i, s := range sources {
		if s.id == id {
			return i
		}
	}
	return -1
}

func findSessions(ctx context.Context, h *pluginsdk.Host, workspaceID, query string) ([]searchItem, error) {
	var hits []struct {
		Session  string `json:"session"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Worktree string `json:"worktree"`
	}
	if err := h.RunCommand(ctx, "agents.search", workspaceID, map[string]string{"query": query}, &hits); err != nil {
		return nil, err
	}
	items := make([]searchItem, 0, len(hits))
	for _, s := range hits {
		line := s.Status
		if s.Worktree != "" {
			line = filepath.Base(s.Worktree) + lineSep + s.Status
		}
		items = append(items, searchItem{Kind: "session", ID: s.Session, Title: cmp.Or(s.Title, s.Session), Line: line, Target: s.Session})
	}
	return items, nil
}

func findTasks(ctx context.Context, h *pluginsdk.Host, workspaceID, query string) ([]searchItem, error) {
	var out struct {
		Tasks []struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := h.RunCommand(ctx, "backlog.list", workspaceID, map[string]any{"query": query, "slim": true}, &out); err != nil {
		return nil, err
	}
	items := make([]searchItem, 0, len(out.Tasks))
	for _, t := range out.Tasks {
		id := strconv.Itoa(t.ID)
		items = append(items, searchItem{Kind: "task", ID: id, Title: t.Title, Line: "#" + id + lineSep + t.Status, Target: id})
	}
	return items, nil
}

func findWikiPages(ctx context.Context, h *pluginsdk.Host, workspaceID, query string) ([]searchItem, error) {
	ws, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil || ws.Home || len(ws.Folders) == 0 {
		return nil, err
	}
	var out struct {
		Hits []struct {
			Page  string `json:"page"`
			Title string `json:"title"`
		} `json:"hits"`
	}
	if err := h.RunCommand(ctx, "wiki.search", workspaceID, map[string]any{"query": query, "limit": wikiHitLimit}, &out); err != nil {
		return nil, err
	}
	if len(out.Hits) == 0 {
		return nil, nil
	}
	project, ok, err := wikiroot.Find(ctx, wikiFolder(ws))
	if err != nil {
		return nil, err
	}
	items := make([]searchItem, 0, len(out.Hits))
	for _, p := range out.Hits {
		item := searchItem{Kind: "wiki", ID: p.Page, Title: cmp.Or(p.Title, p.Page), Target: p.Page}
		if ok {
			item.ID, item.Line, item.Target = project.Name+"/"+p.Page, project.Name, filepath.Join(project.Dir, p.Page)
		}
		items = append(items, item)
	}
	return items, nil
}

func wikiFolder(ws pluginsdk.Workspace) string {
	for _, f := range ws.Folders {
		if _, ok := gitrepo.RepoRoot(gitrepo.Canonical(f.Root())); ok {
			return f.Root()
		}
	}
	return ws.Folders[0].Root()
}

func findDocs(ctx context.Context, h *pluginsdk.Host, workspaceID, query string) ([]searchItem, error) {
	var out struct {
		Docs []struct {
			Path string `json:"path"`
			Rel  string `json:"rel"`
		} `json:"docs"`
	}
	if err := h.RunCommand(ctx, "docs.list", workspaceID, map[string]any{}, &out); err != nil {
		return nil, err
	}
	needle := strings.ToLower(query)
	var items []searchItem
	for _, d := range out.Docs {
		name := d.Rel
		if filepath.IsAbs(name) {
			name = filepath.Base(name)
		}
		if strings.Contains(strings.ToLower(name), needle) {
			items = append(items, searchItem{Kind: "doc", ID: d.Path, Title: filepath.Base(d.Rel), Line: d.Rel, Target: d.Path})
		}
	}
	return items, nil
}
