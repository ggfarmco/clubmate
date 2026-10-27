package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	refreshDelay  = 300 * time.Millisecond
	openCommand   = "editor-nvim.open"
	editorSection = "editor-nvim"
	editorSlot    = "editor-nvim.main"
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errOutsideRepos   = errors.New("path is outside the project repositories")
)

type projectEvent struct {
	ProjectID string `json:"projectId"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type pathEvent struct {
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
}

type pathArgs struct {
	Path string `json:"path"`
}

type state struct {
	mu       sync.Mutex
	synced   bool
	dead     bool
	pending  bool
	running  bool
	dirty    bool
	renderMu sync.Mutex
	repos    []string
	gitDirs  []string
	hideIgn  bool
	expanded map[string]bool
	selected string
	status   map[string]map[string]string
	ignored  map[string]map[string]bool
	watch    string
}

type app struct {
	mu       sync.Mutex
	projects map[string]*state
}

func main() {
	a := &app{projects: map[string]*state{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.*", "project.*", "editor.buffer"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	for _, id := range present {
		a.open(ctx, h, id)
	}
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "ui.opened", "ui.idle":
		var p projectEvent
		if !decodeEvent(ev, &p) {
			return
		}
		if ev.Topic == "ui.opened" {
			a.open(ctx, h, p.ProjectID)
		} else {
			a.idle(ctx, h, p.ProjectID)
		}
	case "project.created", "project.updated":
		var p projectPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.projectChanged(ctx, h, p.Project.ID, ev.Topic == "project.updated")
	case "project.deleted":
		var p deletedPayload
		if !decodeEvent(ev, &p) {
			return
		}
		a.drop(ctx, h, p.ID)
	case "editor.buffer":
		var p pathEvent
		if !decodeEvent(ev, &p) {
			return
		}
		logError("reveal", a.reveal(ctx, h, p.ProjectID, p.Path))
	}
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	s := a.lookup(ev.ProjectID)
	if s == nil {
		return
	}
	s.mu.Lock()
	relevant := s.watch != "" && s.watch == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return containing(s.repos, p) != "" || containing(s.gitDirs, p) != "" }))
	s.mu.Unlock()
	if relevant {
		a.schedule(h, ev.ProjectID, s, refreshDelay)
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	var item string
	if err := json.Unmarshal(ev.Value, &item); err != nil {
		return nil, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	s := a.get(ev.ProjectID)
	if err := a.ensure(ctx, h, ev.ProjectID, s); err != nil {
		return nil, err
	}
	switch ev.Action {
	case "select":
		return nil, a.selectItem(ctx, h, ev.ProjectID, s, item)
	case "expand", "collapse":
		return nil, a.setExpanded(ctx, h, ev.ProjectID, s, item, ev.Action == "expand")
	}
	return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "files.refresh":
		s := a.get(cmd.ProjectID)
		if err := a.sync(ctx, h, cmd.ProjectID, s); err != nil {
			return nil, err
		}
		a.schedule(h, cmd.ProjectID, s, 0)
		return nil, nil
	case "files.toggle-ignored":
		return nil, a.toggleIgnored(ctx, h, cmd.ProjectID)
	case "files.reveal":
		var args pathArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("reveal args: %w", err)
		}
		return nil, a.reveal(ctx, h, cmd.ProjectID, args.Path)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) lookup(id string) *state {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.projects[id]
}

func (a *app) get(id string) *state {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.projects[id]
	if !ok {
		s = &state{expanded: map[string]bool{}}
		a.projects[id] = s
	}
	return s
}

func (a *app) open(ctx context.Context, h *pluginsdk.Host, id string) {
	s := a.get(id)
	if err := a.sync(ctx, h, id, s); err != nil {
		logError("open "+id, err)
		return
	}
	logError("watch "+id, a.rewatch(ctx, h, id, s))
	logError("render "+id, a.render(ctx, h, id, s))
	a.schedule(h, id, s, 0)
}

func (a *app) idle(ctx context.Context, h *pluginsdk.Host, id string) {
	s := a.get(id)
	s.mu.Lock()
	watch := s.watch
	s.watch = ""
	s.mu.Unlock()
	if watch != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, watch))
	}
}

func (a *app) projectChanged(ctx context.Context, h *pluginsdk.Host, id string, updated bool) {
	s := a.get(id)
	if err := a.sync(ctx, h, id, s); err != nil {
		logError("sync "+id, err)
		return
	}
	s.mu.Lock()
	watching := s.watch != ""
	s.mu.Unlock()
	if updated && watching {
		logError("watch "+id, a.rewatch(ctx, h, id, s))
	}
	logError("render "+id, a.render(ctx, h, id, s))
	if watching {
		a.schedule(h, id, s, 0)
	}
}

func (a *app) drop(ctx context.Context, h *pluginsdk.Host, id string) {
	a.mu.Lock()
	s := a.projects[id]
	delete(a.projects, id)
	a.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dead = true
	watch := s.watch
	s.watch = ""
	s.mu.Unlock()
	if watch != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, watch))
	}
	logError("drop "+id, errors.Join(h.StorageDelete(ctx, expandedKey(id)), h.StorageDelete(ctx, selectedKey(id)), h.StorageDelete(ctx, hideIgnoredKey(id))))
}

func (a *app) rewatch(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	old := s.watch
	s.watch = ""
	s.mu.Unlock()
	if old != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, old))
	}
	watch, err := h.WatchFS(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.watch = watch
	s.mu.Unlock()
	return nil
}

func (a *app) ensure(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	synced := s.synced
	s.mu.Unlock()
	if synced {
		return nil
	}
	return a.sync(ctx, h, id, s)
}

func (a *app) sync(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		return err
	}
	repos := make([]string, 0, len(p.Repos))
	for _, r := range p.Repos {
		repos = append(repos, resolve(r))
	}
	var gitDirs []string
	for _, r := range repos {
		gitDir, commonDir, err := gitrepo.Dirs(ctx, r)
		if err != nil {
			continue
		}
		gitDirs = append(gitDirs, gitDir, commonDir)
	}
	s.mu.Lock()
	first := !s.synced
	known := s.repos
	s.mu.Unlock()
	var stored []string
	var found bool
	var selected, hide string
	if first {
		if _, err = h.StorageGet(ctx, hideIgnoredKey(id), &hide); err != nil {
			return err
		}
		if found, err = h.StorageGet(ctx, expandedKey(id), &stored); err != nil {
			return err
		}
		if _, err = h.StorageGet(ctx, selectedKey(id), &selected); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos, s.gitDirs = repos, gitDirs
	if first {
		s.synced, s.selected, s.hideIgn = true, selected, hide == "true"
		for _, e := range stored {
			s.expanded[e] = true
		}
		known = nil
		if found {
			known = repos
		}
	}
	changed := false
	for _, r := range repos {
		if !slices.Contains(known, r) && !s.expanded[r] {
			s.expanded[r] = true
			changed = true
		}
	}
	if changed {
		return h.StorageSet(ctx, expandedKey(id), sortedKeys(s.expanded))
	}
	return nil
}

func (a *app) schedule(h *pluginsdk.Host, id string, s *state, delay time.Duration) {
	s.mu.Lock()
	if s.pending || s.dead {
		s.mu.Unlock()
		return
	}
	s.pending = true
	s.mu.Unlock()
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		s.pending = false
		if s.running {
			s.dirty = true
			s.mu.Unlock()
			return
		}
		s.running = true
		s.mu.Unlock()
		for {
			logError("refresh "+id, a.refresh(context.Background(), h, id, s))
			s.mu.Lock()
			if !s.dirty {
				s.running = false
				s.mu.Unlock()
				return
			}
			s.dirty = false
			s.mu.Unlock()
		}
	})
}

func (a *app) refresh(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	repos := slices.Clone(s.repos)
	expanded := maps.Clone(s.expanded)
	s.mu.Unlock()
	status := map[string]map[string]string{}
	ignored := map[string]map[string]bool{}
	for _, repo := range repos {
		_, entries, err := gitrepo.Status(ctx, repo, false)
		if err != nil {
			logError("status "+repo, err)
		} else {
			status[repo] = statusMap(entries)
		}
		found, err := collectIgnored(ctx, repo, expanded)
		if err != nil {
			logError("ignored "+repo, err)
		} else {
			ignored[repo] = found
		}
	}
	s.mu.Lock()
	for _, repo := range repos {
		if _, ok := status[repo]; !ok {
			status[repo] = s.status[repo]
		}
		if _, ok := ignored[repo]; !ok {
			ignored[repo] = s.ignored[repo]
		}
	}
	s.status, s.ignored = status, ignored
	s.mu.Unlock()
	return a.render(ctx, h, id, s)
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return nil
	}
	roots := make([]Root, len(s.repos))
	for i, r := range s.repos {
		roots[i] = Root{Path: r}
	}
	status := map[string]string{}
	for _, repo := range s.repos {
		maps.Copy(status, s.status[repo])
		for p := range s.ignored[repo] {
			status[p] = ignoredCode
		}
	}
	expanded, selected, hide := maps.Clone(s.expanded), s.selected, s.hideIgn
	s.mu.Unlock()
	_, err := h.Render(ctx, id, sidebarSlot, treeView(buildTree(roots, expanded, status, hide), selected))
	return err
}

func (a *app) selectItem(ctx context.Context, h *pluginsdk.Host, id string, s *state, item string) error {
	if strings.HasSuffix(item, moreSuffix) {
		return nil
	}
	s.mu.Lock()
	inside := containing(s.repos, item) != ""
	expanded := s.expanded[item]
	s.mu.Unlock()
	if !inside {
		return fmt.Errorf("%w: %q", errOutsideRepos, item)
	}
	link, err := os.Lstat(item)
	if err != nil {
		return err
	}
	if info, err := os.Stat(item); err == nil && info.IsDir() {
		if link.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return a.setExpanded(ctx, h, id, s, item, !expanded)
	}
	s.mu.Lock()
	s.selected = item
	s.mu.Unlock()
	if err := h.StorageSet(ctx, selectedKey(id), item); err != nil {
		return err
	}
	if err := a.render(ctx, h, id, s); err != nil {
		return err
	}
	if err := h.RunCommand(ctx, openCommand, id, pathArgs{Path: item}, nil); err != nil {
		return err
	}
	logError("focus "+id, h.Focus(ctx, id, editorSection, editorSlot))
	return nil
}

func (a *app) toggleIgnored(ctx context.Context, h *pluginsdk.Host, id string) error {
	s := a.get(id)
	if err := a.ensure(ctx, h, id, s); err != nil {
		return err
	}
	s.mu.Lock()
	s.hideIgn = !s.hideIgn
	hide := s.hideIgn
	s.mu.Unlock()
	var err error
	if hide {
		err = h.StorageSet(ctx, hideIgnoredKey(id), "true")
	} else {
		err = h.StorageDelete(ctx, hideIgnoredKey(id))
	}
	if err != nil {
		return err
	}
	return a.render(ctx, h, id, s)
}

func (a *app) setExpanded(ctx context.Context, h *pluginsdk.Host, id string, s *state, item string, on bool) error {
	s.mu.Lock()
	if on {
		s.expanded[item] = true
	} else {
		delete(s.expanded, item)
	}
	keys := sortedKeys(s.expanded)
	s.mu.Unlock()
	if err := h.StorageSet(ctx, expandedKey(id), keys); err != nil {
		return err
	}
	if on {
		a.classify(ctx, s, item)
	}
	return a.render(ctx, h, id, s)
}

func (a *app) classify(ctx context.Context, s *state, dir string) {
	s.mu.Lock()
	repo := containing(s.repos, dir)
	muted := repo == ""
	for p := dir; !muted && len(p) >= len(repo); p = filepath.Dir(p) {
		muted = s.ignored[repo][p]
	}
	s.mu.Unlock()
	if muted {
		return
	}
	entries := readEntries(dir)
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = filepath.Join(dir, e.name)
	}
	found, err := gitrepo.Ignored(ctx, repo, paths)
	if err != nil {
		logError("ignored "+dir, err)
		return
	}
	s.mu.Lock()
	if s.ignored == nil {
		s.ignored = map[string]map[string]bool{}
	}
	if s.ignored[repo] == nil {
		s.ignored[repo] = map[string]bool{}
	}
	maps.Copy(s.ignored[repo], found)
	s.mu.Unlock()
}

func (a *app) reveal(ctx context.Context, h *pluginsdk.Host, id, path string) error {
	s := a.get(id)
	if err := a.ensure(ctx, h, id, s); err != nil {
		return err
	}
	real := resolve(path)
	s.mu.Lock()
	repo := containing(s.repos, real)
	if repo == "" {
		s.mu.Unlock()
		return nil
	}
	s.selected = real
	var opened []string
	for dir := filepath.Dir(real); len(dir) >= len(repo); dir = filepath.Dir(dir) {
		if !s.expanded[dir] {
			opened = append(opened, dir)
		}
		s.expanded[dir] = true
	}
	keys := sortedKeys(s.expanded)
	s.mu.Unlock()
	if err := errors.Join(h.StorageSet(ctx, expandedKey(id), keys), h.StorageSet(ctx, selectedKey(id), real)); err != nil {
		return err
	}
	for _, dir := range slices.Backward(opened) {
		a.classify(ctx, s, dir)
	}
	return a.render(ctx, h, id, s)
}

func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	dir, base := filepath.Split(filepath.Clean(path))
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(real, base)
	}
	return filepath.Clean(path)
}

func containing(repos []string, path string) string {
	best := ""
	for _, r := range repos {
		if (path == r || strings.HasPrefix(path, r+"/")) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func expandedKey(projectID string) string {
	return "expanded/" + projectID
}

func hideIgnoredKey(projectID string) string {
	return "hideIgnored/" + projectID
}

func selectedKey(projectID string) string {
	return "selected/" + projectID
}

func decodeEvent(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "files:", what+":", err)
	}
}
