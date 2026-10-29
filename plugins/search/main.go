package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
)

const (
	defaultLimit     = 50
	maxLimit         = 200
	coldWait         = 3 * time.Second
	defaultGrepLimit = 100
	maxGrepLimit     = 500
	grepTimeout      = 4 * time.Second
)

var (
	errUnknownCommand = errors.New("unknown command")
	errNoProject      = errors.New("command needs a project")
	errEmptyQuery     = errors.New("query is empty")
	errOutside        = errors.New("path is outside the workspace folders")
	errHome           = &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "search unavailable in the home workspace"}
)

type filesArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type fileMatch struct {
	Path      string `json:"path"`
	AbsPath   string `json:"absPath"`
	Positions []int  `json:"positions"`
	Line      int    `json:"line,omitempty"`
}

type filesResult struct {
	Matches   []fileMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
	Capped    bool        `json:"capped"`
	Indexing  bool        `json:"indexing,omitempty"`
}

type startResult struct {
	ID string `json:"id"`
}

type pollArgs struct {
	ID    string `json:"id"`
	After int    `json:"after"`
}

type grepArgs struct {
	grepOptions
	Limit int `json:"limit"`
}

type grepResult struct {
	Matches   []grepMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
}

type previewArgs struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Context *int   `json:"context"`
}

type previewResult struct {
	Lines []previewLine `json:"lines"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type listing struct {
	folders  []string
	resolved []string
	labels   []string
	corpus   corpus
	capped   bool
}

type workspace struct {
	mu       sync.Mutex
	folders  []string
	list     *listing
	builtAt  int64
	building chan struct{}
	err      error
	gen      atomic.Int64
	watch    string
}

type app struct {
	idx        index
	grep       *searches
	mu         sync.Mutex
	workspaces map[string]*workspace
}

func main() {
	a := &app{idx: newIndex(), grep: newSearches(), workspaces: map[string]*workspace{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	_, err := h.Subscribe(ctx, "project.deleted", math.MaxInt64)
	return err
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ProjectID == "" {
		return nil, errNoProject
	}
	switch cmd.ID {
	case "search.files":
		var args filesArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.searchFiles(ctx, h, cmd.ProjectID, args)
	case "search.start":
		var args grepOptions
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.startGrep(ctx, h, cmd.ProjectID, args)
	case "search.poll":
		var args pollArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.grep.poll(ctx, cmd.ProjectID, args.ID, args.After), nil
	case "search.cancel":
		var args pollArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		a.grep.cancel(cmd.ProjectID, args.ID)
		return struct{}{}, nil
	case "search.preview":
		var args previewArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.preview(ctx, h, cmd.ProjectID, args)
	case "search.grep":
		var args grepArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.grepNow(ctx, h, cmd.ProjectID, args)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	var p deletedPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError("decode "+ev.Topic, err)
		return
	}
	a.mu.Lock()
	ws := a.workspaces[p.ID]
	delete(a.workspaces, p.ID)
	watch := ""
	if ws != nil {
		watch, ws.watch = ws.watch, ""
	}
	a.mu.Unlock()
	a.grep.drop(p.ID)
	if watch != "" {
		logError("unwatch "+p.ID, h.UnwatchFS(ctx, watch))
	}
}

func (a *app) fsChanged(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.FSChange) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws := a.workspaces[ev.ProjectID]
	if ws != nil && ws.watch == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, outsideGitDir)) {
		ws.gen.Add(1)
	}
}

func (a *app) searchFiles(ctx context.Context, h *pluginsdk.Host, projectID string, args filesArgs) (filesResult, error) {
	p, err := project(ctx, h, projectID)
	if err != nil {
		return filesResult{}, err
	}
	timer := time.NewTimer(coldWait)
	defer timer.Stop()
	list, err := a.listing(ctx, h, p, timer.C)
	if err != nil {
		return filesResult{}, err
	}
	if list == nil {
		return filesResult{Matches: []fileMatch{}, Indexing: true}, nil
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	query, line := splitLine(args.Query)
	ranked := list.corpus.rank(query, limit+1)
	res := filesResult{Truncated: len(ranked) > limit, Capped: list.capped, Matches: make([]fileMatch, 0, min(len(ranked), limit))}
	for _, m := range ranked[:min(len(ranked), limit)] {
		res.Matches = append(res.Matches, fileMatch{Path: m.Path, AbsPath: list.abs(m.Path), Positions: m.Positions, Line: line})
	}
	return res, nil
}

func (a *app) startGrep(ctx context.Context, h *pluginsdk.Host, projectID string, opts grepOptions) (startResult, error) {
	p, err := project(ctx, h, projectID)
	if err != nil {
		return startResult{}, err
	}
	if opts.Query == "" {
		return startResult{}, errEmptyQuery
	}
	id := a.grep.start(p.ID, func(ctx context.Context) (engine, error) { return a.engine(ctx, h, p) }, opts)
	return startResult{ID: id}, nil
}

func (a *app) grepNow(ctx context.Context, h *pluginsdk.Host, projectID string, args grepArgs) (grepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, grepTimeout)
	defer cancel()
	p, err := project(ctx, h, projectID)
	if err != nil {
		return grepResult{}, err
	}
	if args.Query == "" {
		return grepResult{}, errEmptyQuery
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultGrepLimit
	}
	eng, err := a.engine(ctx, h, p)
	if err != nil {
		if ctx.Err() != nil {
			return grepResult{Matches: []grepMatch{}, Truncated: true}, nil
		}
		return grepResult{}, err
	}
	matches, truncated, err := collect(ctx, eng, args.grepOptions, min(limit, maxGrepLimit))
	return grepResult{Matches: matches, Truncated: truncated}, err
}

func (a *app) engine(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Project) (engine, error) {
	roots := newRoots(p.Repos)
	if bin, err := exec.LookPath("rg"); err == nil && !slices.ContainsFunc(roots, func(r root) bool { return a.idx.guarded(r.resolved) }) {
		return rgEngine{bin: bin, roots: roots}, nil
	}
	list, err := a.listing(ctx, h, p, nil)
	if err != nil {
		return nil, err
	}
	return builtinEngine{list: list}, nil
}

func (a *app) preview(ctx context.Context, h *pluginsdk.Host, projectID string, args previewArgs) (previewResult, error) {
	p, err := project(ctx, h, projectID)
	if err != nil {
		return previewResult{}, err
	}
	path, err := filepath.EvalSymlinks(args.Path)
	if err != nil {
		return previewResult{}, err
	}
	if !slices.ContainsFunc(newRoots(p.Repos), func(r root) bool { return strings.HasPrefix(path, r.resolved+"/") }) {
		return previewResult{}, errOutside
	}
	around := defaultContext
	if args.Context != nil {
		around = min(max(*args.Context, 0), maxContext)
	}
	lines, err := preview(path, max(args.Line, 1), around)
	return previewResult{Lines: lines}, err
}

func (a *app) listing(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Project, cold <-chan time.Time) (*listing, error) {
	a.mu.Lock()
	ws, ok := a.workspaces[p.ID]
	if !ok {
		ws = &workspace{}
		a.workspaces[p.ID] = ws
	}
	a.mu.Unlock()
	ws.mu.Lock()
	if !slices.Equal(ws.folders, p.Repos) || !ws.watched(a) {
		if err := a.rewatch(ctx, h, p.ID, ws); err != nil {
			ws.mu.Unlock()
			return nil, err
		}
		ws.folders, ws.list = slices.Clone(p.Repos), nil
	}
	list, done := ws.list, a.revalidate(ctx, ws)
	ws.mu.Unlock()
	if list != nil {
		return list, nil
	}
	select {
	case <-done:
	case <-cold:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.list != nil {
		return ws.list, nil
	}
	return nil, ws.err
}

func (a *app) revalidate(ctx context.Context, ws *workspace) <-chan struct{} {
	gen := ws.gen.Load()
	if ws.building != nil || ws.list != nil && ws.builtAt == gen {
		return ws.building
	}
	done := make(chan struct{})
	ws.building = done
	go a.build(context.WithoutCancel(ctx), ws, ws.folders, gen, done)
	return done
}

func (a *app) build(ctx context.Context, ws *workspace, folders []string, gen int64, done chan struct{}) {
	defer close(done)
	paths, err := a.idx.Files(ctx, folders)
	logError("index", err)
	var list *listing
	if err == nil {
		list = newListing(folders, paths)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.building = nil
	if !slices.Equal(ws.folders, folders) {
		return
	}
	ws.err = err
	if list != nil {
		ws.list, ws.builtAt = list, gen
	}
}

func (a *app) rewatch(ctx context.Context, h *pluginsdk.Host, projectID string, ws *workspace) error {
	a.mu.Lock()
	old := ws.watch
	ws.watch = ""
	a.mu.Unlock()
	if old != "" {
		logError("unwatch "+projectID, h.UnwatchFS(ctx, old))
	}
	watch, err := h.WatchFS(ctx, projectID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	ws.watch = watch
	a.mu.Unlock()
	return nil
}

func (ws *workspace) watched(a *app) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ws.watch != ""
}

func newListing(folders, paths []string) *listing {
	resolved := make([]string, len(folders))
	for i, f := range folders {
		resolved[i] = f
		if real, err := filepath.EvalSymlinks(f); err == nil {
			resolved[i] = real
		}
	}
	return &listing{folders: folders, resolved: resolved, labels: labels(folders), corpus: newCorpus(paths), capped: len(paths) >= maxFiles}
}

func (l *listing) abs(path string) string {
	if i, rel := l.locate(path); i >= 0 {
		return filepath.Join(l.folders[i], rel)
	}
	return ""
}

func (l *listing) locate(path string) (int, string) {
	for i, label := range l.labels {
		if label == "" {
			return i, path
		}
		if rel, ok := strings.CutPrefix(path, label+"/"); ok {
			return i, rel
		}
	}
	return -1, path
}

func project(ctx context.Context, h *pluginsdk.Host, id string) (pluginsdk.Project, error) {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		return pluginsdk.Project{}, err
	}
	if p.Home {
		return pluginsdk.Project{}, errHome
	}
	return p, nil
}

func decodeArgs(cmd pluginsdk.CommandRun, v any) error {
	if len(cmd.Args) == 0 {
		return nil
	}
	if err := json.Unmarshal(cmd.Args, v); err != nil {
		return fmt.Errorf("%s args: %w", cmd.ID, err)
	}
	return nil
}

func splitLine(query string) (string, int) {
	i := strings.LastIndexByte(query, ':')
	if i < 0 {
		return query, 0
	}
	line, err := strconv.Atoi(query[i+1:])
	if err != nil || line <= 0 {
		return query, 0
	}
	return query[:i], line
}

func outsideGitDir(path string) bool {
	return !strings.Contains(path, "/.git/") && !strings.HasSuffix(path, "/.git")
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %s: %v\n", what, err)
	}
}
