package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	mutateTimeout = 10 * time.Minute
	currentHead   = "HEAD"
)

var (
	errUnknownCommand = errors.New("unknown command")
	errUnknownRepo    = errors.New("unknown repository")
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

type state struct {
	mu      sync.Mutex
	dead    bool
	pending bool
	running bool
	dirty   bool
	repos   []string
	gitDirs []string
	busy    string
	watch   string
	missing []string
}

type app struct {
	mu       sync.Mutex
	projects map[string]*state
	ops      opTable
}

func main() {
	a := &app{projects: map[string]*state{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, FSChanged: a.fsChanged})
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
	for _, pattern := range []string{"ui.*", "project.*"} {
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
	}
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	s := a.lookup(ev.ProjectID)
	if s == nil {
		return
	}
	s.mu.Lock()
	relevant := s.watch != "" && s.watch == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return containing(s.repos, p) || containing(s.gitDirs, p) }))
	s.mu.Unlock()
	if relevant {
		a.schedule(h, ev.ProjectID, s, refreshDelay)
	}
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	s := a.get(cmd.ProjectID)
	if err := a.sync(ctx, h, cmd.ProjectID, s); err != nil {
		return nil, err
	}
	if strings.HasPrefix(cmd.ID, "git.worktree") || strings.HasPrefix(cmd.ID, "git.branch") {
		return a.worktreeCommand(ctx, h, cmd, s)
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
	skip := s.busy != "" || slices.Equal(s.missing, p.Missing)
	if !skip {
		s.missing = p.Missing
	}
	s.mu.Unlock()
	if !skip && len(p.Missing) > 0 {
		_, err = h.CheckWorktrees(ctx, id)
		logError("check worktrees "+id, err)
	}
}

func mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Mutate(ctx, repo, append([]string{"--literal-pathspecs"}, args...)...)
	if errors.Is(err, exec.ErrWaitDelay) {
		return out, nil
	}
	return out, gitError(out, err)
}

func run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Run(ctx, repo, append([]string{"--literal-pathspecs"}, args...)...)
	return out, gitError(out, err)
}

func gitError(out []byte, err error) error {
	ee, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return err
	}
	for _, msg := range [][]byte{ee.Stderr, out} {
		if text := strings.TrimSpace(string(msg)); text != "" {
			return errors.New(text)
		}
	}
	return err
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

func decodeEvent(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "git:", what+":", err)
	}
}
