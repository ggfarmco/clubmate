package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

var (
	errUnknownCommand = errors.New("unknown command")
	errNoProject      = errors.New("command needs a project")
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
}

type deletedPayload struct {
	ID string `json:"id"`
}

type listing struct {
	folders []string
	labels  []string
	paths   []string
}

type workspace struct {
	mu      sync.Mutex
	list    listing
	built   bool
	builtAt int64
	gen     atomic.Int64
	watch   string
}

type app struct {
	idx        index
	mu         sync.Mutex
	workspaces map[string]*workspace
}

func main() {
	a := &app{idx: newIndex(), workspaces: map[string]*workspace{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	_, err := h.Subscribe(ctx, "project.deleted", math.MaxInt64)
	return err
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID != "search.files" {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
	}
	if cmd.ProjectID == "" {
		return nil, errNoProject
	}
	var args filesArgs
	if err := json.Unmarshal(cmd.Args, &args); err != nil {
		return nil, fmt.Errorf("files args: %w", err)
	}
	return a.searchFiles(ctx, h, cmd.ProjectID, args)
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
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return filesResult{}, err
	}
	if p.Home {
		return filesResult{}, errHome
	}
	list, err := a.listing(ctx, h, p)
	if err != nil {
		return filesResult{}, err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	query, line := splitLine(args.Query)
	ranked := rank(list.paths, query, limit+1)
	res := filesResult{Truncated: len(ranked) > limit || len(list.paths) >= maxFiles, Matches: make([]fileMatch, 0, min(len(ranked), limit))}
	for _, m := range ranked[:min(len(ranked), limit)] {
		res.Matches = append(res.Matches, fileMatch{Path: m.Path, AbsPath: list.abs(m.Path), Positions: m.Positions, Line: line})
	}
	return res, nil
}

func (a *app) listing(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Project) (listing, error) {
	a.mu.Lock()
	ws, ok := a.workspaces[p.ID]
	if !ok {
		ws = &workspace{}
		a.workspaces[p.ID] = ws
	}
	a.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if !slices.Equal(ws.list.folders, p.Repos) || !ws.watched(a) {
		if err := a.rewatch(ctx, h, p.ID, ws); err != nil {
			return listing{}, err
		}
		ws.list, ws.built = listing{folders: slices.Clone(p.Repos), labels: labels(p.Repos)}, false
	}
	gen := ws.gen.Load()
	if ws.built && ws.builtAt == gen {
		return ws.list, nil
	}
	paths, err := a.idx.Files(ctx, ws.list.folders)
	if err != nil {
		return listing{}, err
	}
	ws.list.paths, ws.built, ws.builtAt = paths, true, gen
	return ws.list, nil
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

func (l listing) abs(path string) string {
	for i, label := range l.labels {
		if label == "" {
			return filepath.Join(l.folders[i], path)
		}
		if rel, ok := strings.CutPrefix(path, label+"/"); ok {
			return filepath.Join(l.folders[i], rel)
		}
	}
	return ""
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
