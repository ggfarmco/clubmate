package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	refreshDelay = 300 * time.Millisecond
	changedTopic = "git.changed"
)

type changedEvent struct {
	Workspace string `json:"workspace"`
}

type state struct {
	mu      sync.Mutex
	dead    bool
	pending bool
	running bool
	dirty   bool
	head    bool
	repos   []string
	gitDirs []string
	busy    bool
	watch   string
	missing []string
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
		s = &state{}
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
	a.schedule(h, id, s, 0)
}

func (a *app) idle(ctx context.Context, h *pluginsdk.Host, id string) {
	unwatch(ctx, h, id, a.get(id).takeWatch())
}

func (a *app) projectChanged(ctx context.Context, h *pluginsdk.Host, id string, updated bool) {
	s := a.get(id)
	if err := a.sync(ctx, h, id, s); err != nil {
		logError("sync "+id, err)
		return
	}
	watching := s.watching()
	if updated && watching {
		logError("watch "+id, a.rewatch(ctx, h, id, s))
	}
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
	unwatch(ctx, h, id, watch)
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	s := a.lookup(ev.ProjectID)
	if s == nil {
		return
	}
	s.mu.Lock()
	relevant := s.watch != "" && s.watch == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return containing(s.repos, p) || containing(s.gitDirs, p) }))
	if relevant && (ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return filepath.Base(p) == currentHead && containing(s.gitDirs, p) })) {
		s.head = true
	}
	s.mu.Unlock()
	if relevant {
		a.schedule(h, ev.ProjectID, s, refreshDelay)
	}
}

func (a *app) rewatch(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	unwatch(ctx, h, id, s.takeWatch())
	watch, err := h.WatchFS(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.watch = watch
	s.mu.Unlock()
	return nil
}

func (a *app) sync(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		return err
	}
	repos := make([]string, 0, len(p.Roots))
	for _, r := range p.Roots {
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
	defer s.mu.Unlock()
	s.repos, s.gitDirs = repos, gitDirs
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
			a.checkMissing(context.Background(), h, id, s)
			a.publishHead(context.Background(), h, id, s)
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

func (a *app) checkMissing(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		logError("project "+id, err)
		return
	}
	s.mu.Lock()
	skip := s.busy || slices.Equal(s.missing, p.Missing)
	if !skip {
		s.missing = p.Missing
	}
	s.mu.Unlock()
	if !skip && len(p.Missing) > 0 {
		_, err = h.CheckWorktrees(ctx, id)
		logError("check worktrees "+id, err)
	}
}

func (a *app) publishHead(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	s.mu.Lock()
	head := s.head
	s.head = false
	s.mu.Unlock()
	if head {
		_, err := h.Publish(ctx, changedTopic, changedEvent{Workspace: id})
		logError("publish "+changedTopic, err)
	}
}

func (s *state) claim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *state) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
}

func (s *state) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

func (s *state) watching() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watch != ""
}

func (s *state) takeWatch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	watch := s.watch
	s.watch = ""
	return watch
}

func unwatch(ctx context.Context, h *pluginsdk.Host, id, watch string) {
	if watch != "" {
		logError("unwatch "+id, h.UnwatchFS(ctx, watch))
	}
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

func containing(roots []string, path string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return path == r || strings.HasPrefix(path, r+"/") })
}
