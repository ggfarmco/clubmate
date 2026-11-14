package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/docext"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/fileindex"
	"clubmate/plugins/internal/gitrepo"
)

const (
	maxListed = 2000
	orderKey  = "#order"
)

type listArgs struct {
	Session     string  `json:"session,omitempty"`
	IfNoneMatch string  `json:"ifNoneMatch,omitempty"`
	Since       *string `json:"since,omitempty"`
}

type listItem struct {
	Path     string `json:"path"`
	Rel      string `json:"rel"`
	Kind     string `json:"kind"`
	Modified string `json:"modified"`
	Agent    bool   `json:"agent"`
}

type listResult struct {
	Docs      []listItem `json:"docs"`
	Truncated bool       `json:"truncated"`
	Etag      string     `json:"etag"`
}

type sinceResult struct {
	Docs      []listItem `json:"docs"`
	Removed   []string   `json:"removed"`
	Order     []string   `json:"order,omitempty"`
	Cursor    string     `json:"cursor"`
	Delta     bool       `json:"delta"`
	Truncated bool       `json:"truncated"`
}

type unchangedResult struct {
	Unchanged bool   `json:"unchanged"`
	Etag      string `json:"etag"`
}

type agentDocs struct {
	Docs []struct {
		Path string `json:"path"`
	} `json:"docs"`
}

type listRoot struct {
	label string
	dirs  []string
}

type lister struct {
	allow func(string) bool
	roots []listRoot
	seen  map[string]bool
	res   listResult
}

func (d *docs) listCmd(ctx context.Context, h *pluginsdk.Host, workspaceID string, args listArgs) (any, error) {
	res, err := d.list(ctx, h, workspaceID, listArgs{Session: args.Session})
	if err != nil {
		return nil, err
	}
	if args.Since != nil {
		return d.since(ctx, workspaceID, args.Session, res, *args.Since), nil
	}
	sum := fnv.New64a()
	if err := json.NewEncoder(sum).Encode(res); err != nil {
		return nil, err
	}
	res.Etag = fmt.Sprintf("%016x", sum.Sum64())
	if args.IfNoneMatch == res.Etag {
		return unchangedResult{Unchanged: true, Etag: res.Etag}, nil
	}
	return res, nil
}

func (d *docs) since(ctx context.Context, workspaceID, session string, res listResult, cursor string) sinceResult {
	revs, name := &d.revs, docsTracker(workspaceID)
	if session != "" {
		revs, name = &d.scoped, name+"/"+session
	}
	items := make(map[string]any, len(res.Docs)+1)
	order := make([]string, 0, len(res.Docs))
	for _, doc := range res.Docs {
		items[doc.Path] = doc
		order = append(order, doc.Path)
	}
	items[orderKey] = order
	v, err := revs.Observe(ctx, name, items, cursor)
	logError("docs delta", err)
	out := sinceResult{Docs: res.Docs, Removed: []string{}, Cursor: v.Cursor, Truncated: res.Truncated}
	if v.Full {
		return out
	}
	out.Delta = true
	out.Docs = slices.DeleteFunc(res.Docs, func(doc listItem) bool {
		_, found := slices.BinarySearch(v.Changed, doc.Path)
		return !found
	})
	out.Removed = append(out.Removed, v.Removed...)
	if _, found := slices.BinarySearch(v.Changed, orderKey); found {
		out.Order = order
	}
	return out
}

func docsTracker(workspaceID string) string {
	return "docs/" + workspaceID
}

func (d *docs) list(ctx context.Context, h *pluginsdk.Host, workspaceID string, args listArgs) (listResult, error) {
	ws, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return listResult{}, err
	}
	var written agentDocs
	if err := h.RunCommand(ctx, "agents.docs", ws.ID, args, &written); err != nil {
		if args.Session != "" {
			return listResult{}, err
		}
		logError("agents.docs", err)
	}
	l := newLister(ws, d.scope(ctx, h, ws))
	for _, doc := range written.Docs {
		if !l.add(doc.Path, "", true) {
			return l.res, nil
		}
	}
	if args.Session != "" || ws.Home {
		return l.res, nil
	}
	roots := ws.Roots()
	files, err := d.index.Files(ctx, roots, ws.Paths()...)
	if err != nil {
		return listResult{}, err
	}
	l.res.Truncated = len(files) >= fileindex.MaxFiles
	files = slices.DeleteFunc(files, func(rel string) bool { return !viewable(rel) })
	slices.Sort(files)
	for _, rel := range files {
		if !l.add(l.abs(rel), rel, false) {
			break
		}
	}
	return l.res, nil
}

func newLister(ws contract.Workspace, allow func(string) bool) *lister {
	labels := fileindex.Labels(ws.Roots())
	l := &lister{allow: allow, seen: map[string]bool{}, res: listResult{Docs: []listItem{}}}
	for i, f := range ws.Folders {
		dirs := []string{gitrepo.Canonical(f.Root())}
		if p := gitrepo.Canonical(f.Path); p != dirs[0] {
			dirs = append(dirs, p)
		}
		l.roots = append(l.roots, listRoot{label: labels[i], dirs: dirs})
	}
	return l
}

func (l *lister) add(path, rel string, agent bool) bool {
	abs := gitrepo.Canonical(filepath.Clean(path))
	if l.seen[abs] || !viewable(abs) || !l.allow(abs) {
		return true
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return true
	}
	if len(l.res.Docs) == maxListed {
		l.res.Truncated = true
		return false
	}
	l.seen[abs] = true
	if rel == "" {
		rel = l.rel(abs)
	}
	kind := sdui.DocumentMarkdown
	if docext.IsArtifact(abs) {
		kind = sdui.DocumentArtifact
	}
	l.res.Docs = append(l.res.Docs, listItem{Path: abs, Rel: rel, Kind: kind, Modified: info.ModTime().UTC().Format(time.RFC3339), Agent: agent})
	return true
}

func (l *lister) rel(abs string) string {
	for _, r := range l.roots {
		for _, dir := range r.dirs {
			if rel, err := filepath.Rel(dir, abs); err == nil && gitrepo.Within(abs, dir) {
				return joinLabel(r.label, filepath.ToSlash(rel))
			}
		}
	}
	return abs
}

func (l *lister) abs(rel string) string {
	for _, r := range l.roots {
		if r.label == "" {
			return filepath.Join(r.dirs[0], rel)
		}
		if rest, ok := strings.CutPrefix(rel, r.label+"/"); ok {
			return filepath.Join(r.dirs[0], rest)
		}
	}
	return rel
}

func joinLabel(label, rel string) string {
	if label == "" {
		return rel
	}
	return label + "/" + rel
}
