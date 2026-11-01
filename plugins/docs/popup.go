package main

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	popupSlot = "docs.popup"
	debounce  = 200 * time.Millisecond
	maxReads  = 16
)

type clock interface {
	AfterFunc(d time.Duration, f func()) func() bool
}

type realClock struct{}

type docState struct {
	rev int
}

type entry struct {
	path   string
	anchor string
}

type space struct {
	mu      sync.Mutex
	stack   []entry
	reads   []string
	docs    map[string]*docState
	watch   string
	pending map[string]bool
	stop    func() bool
}

func (d *docs) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Slot != popupSlot {
		return nil, nil
	}
	switch ev.Action {
	case "openDoc":
		return nil, d.openDoc(ctx, h, ev)
	case "openFile":
		return nil, d.openFile(ctx, h, ev)
	case "back":
		return nil, d.back(ctx, h, ev.WorkspaceID)
	case "close":
		return nil, d.close(ctx, h, ev.WorkspaceID)
	}
	return nil, nil
}

func (d *docs) fsChanged(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.FSChange) {
	s := d.lookup(ev.WorkspaceID)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Watch != s.watch {
		return
	}
	for path := range s.docs {
		if ev.Overflow || slices.ContainsFunc(ev.Paths, func(p string) bool { return gitrepo.Within(path, p) }) {
			s.pending[path] = true
		}
	}
	if len(s.pending) > 0 && s.stop == nil {
		s.stop = d.clock.AfterFunc(debounce, func() { d.flush(ev.WorkspaceID) })
	}
}

func (d *docs) openDoc(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	path, err := eventPath(ev)
	if err != nil {
		return err
	}
	path, anchor := splitAnchor(path)
	ws, err := h.GetWorkspace(ctx, ev.WorkspaceID)
	if err != nil {
		return err
	}
	allow := workspaceScope(ctx, ws, d.uploads)
	abs, src, err := load(path, allow)
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	s := d.space(ev.WorkspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) == 0 || s.top() != abs {
		s.stack = append(s.stack, entry{path: abs})
	}
	s.stack[len(s.stack)-1].anchor = anchor
	s.track(abs)
	if err := d.ensureWatch(ctx, h, ev.WorkspaceID, s); err != nil {
		return err
	}
	return show(ctx, h, ev.WorkspaceID, s.viewOf(src, err, allow))
}

func (d *docs) openFile(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) error {
	path, err := eventPath(ev)
	if err != nil {
		return err
	}
	ws, err := h.GetWorkspace(ctx, ev.WorkspaceID)
	if err != nil {
		return err
	}
	if !workspaceScope(ctx, ws, d.uploads)(path) {
		return errNotInWorkspace
	}
	return h.RunCommand(ctx, "editor.open", ev.WorkspaceID, map[string]string{"path": path}, nil)
}

func (d *docs) back(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	ws, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	allow := workspaceScope(ctx, ws, d.uploads)
	s := d.space(workspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) < 2 {
		return nil
	}
	s.stack = s.stack[:len(s.stack)-1]
	s.prune()
	return s.present(ctx, h, workspaceID, allow)
}

func (d *docs) close(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	s := d.space(workspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stack = nil
	s.prune()
	if _, err := h.Render(ctx, workspaceID, popupSlot, sdui.Empty("")); err != nil {
		return err
	}
	if len(s.docs) > 0 {
		return nil
	}
	return d.unwatch(ctx, h, s)
}

func (d *docs) flush(workspaceID string) {
	ctx := context.Background()
	s := d.lookup(workspaceID)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop = nil
	paths := slices.Sorted(maps.Keys(s.pending))
	clear(s.pending)
	if len(paths) == 0 {
		return
	}
	ws, err := d.h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		logError("flush "+workspaceID, err)
		return
	}
	allow := workspaceScope(ctx, ws, d.uploads)
	for _, path := range paths {
		st, ok := s.docs[path]
		if !ok {
			continue
		}
		st.rev++
		if len(s.stack) > 0 && s.top() == path {
			logError("render "+path, s.present(ctx, d.h, workspaceID, allow))
		}
		_, err := d.h.Publish(ctx, changedTopic, changedEvent{Workspace: workspaceID, Path: path, Rev: st.rev})
		logError("publish "+changedTopic, err)
	}
}

func (d *docs) ensureWatch(ctx context.Context, h *pluginsdk.Host, workspaceID string, s *space) error {
	if s.watch != "" {
		return nil
	}
	watch, err := h.WatchFS(ctx, workspaceID)
	if err != nil {
		return err
	}
	s.watch = watch
	return nil
}

func (d *docs) unwatch(ctx context.Context, h *pluginsdk.Host, s *space) error {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	clear(s.pending)
	if s.watch == "" {
		return nil
	}
	watch := s.watch
	s.watch = ""
	return h.UnwatchFS(ctx, watch)
}

func (s *space) track(path string) *docState {
	st, ok := s.docs[path]
	if !ok {
		st = &docState{rev: 1}
		s.docs[path] = st
	}
	return st
}

func (s *space) read(path string) {
	s.reads = append(slices.DeleteFunc(s.reads, func(p string) bool { return p == path }), path)
	if len(s.reads) > maxReads {
		s.reads = s.reads[len(s.reads)-maxReads:]
	}
}

func (s *space) prune() {
	for path := range s.docs {
		if !slices.ContainsFunc(s.stack, func(e entry) bool { return e.path == path }) && !slices.Contains(s.reads, path) {
			delete(s.docs, path)
			delete(s.pending, path)
		}
	}
}

func (s *space) present(ctx context.Context, h *pluginsdk.Host, workspaceID string, allow func(string) bool) error {
	_, src, err := load(s.top(), allow)
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	return show(ctx, h, workspaceID, s.viewOf(src, err, allow))
}

func (s *space) viewOf(src []byte, loadErr error, allow func(string) bool) popupState {
	top := s.stack[len(s.stack)-1]
	path := top.path
	st := s.track(path)
	v := popupState{path: path, anchor: top.anchor, rev: st.rev, back: len(s.stack) > 1, gone: loadErr != nil}
	if !v.gone {
		doc, kind := render(src, path, allow)
		v.html = doc.HTML
		if kind == sdui.DocumentArtifact {
			v.kind = kind
		}
	}
	v.title = filepath.Base(path)
	return v
}

func (s *space) top() string {
	return s.stack[len(s.stack)-1].path
}

func splitAnchor(path string) (string, string) {
	if i := strings.LastIndexByte(path, '#'); i >= 0 && viewable(path[:i]) {
		return path[:i], path[i+1:]
	}
	return path, ""
}

func show(ctx context.Context, h *pluginsdk.Host, workspaceID string, v popupState) error {
	_, err := h.Render(ctx, workspaceID, popupSlot, popupView(v))
	return err
}

func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}
