package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const noName = "[No Name]"

var (
	errUnsaved        = errors.New("unsaved buffers")
	errUnsavedUnknown = fmt.Errorf("%w: unknown (nvim not responding)", errUnsaved)
	errDeleted        = errors.New("project deleted")
	errStopping       = errors.New("the worktree is being removed")
)

func (a *app) ensureLogged(ctx context.Context, h *pluginsdk.Host, projectID string) {
	if a.isDeleted(projectID) {
		return
	}
	e, err := a.active(ctx, h, projectID)
	if e != nil && err == nil {
		_, err = a.ensure(ctx, h, e)
	}
	if err != nil && !quiet(err) {
		logError("start "+projectID, err)
	}
}

func (a *app) ensure(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
	e.life.Lock()
	defer e.life.Unlock()
	return a.ensureLocked(ctx, h, e)
}

func (a *app) ensureLocked(ctx context.Context, h *pluginsdk.Host, e *editor) (*client, error) {
	if a.isDeleted(e.id) {
		return nil, errDeleted
	}
	if a.isStopping(e.id, e.root) {
		return nil, errStopping
	}
	if c := e.connected(); c != nil {
		return c, nil
	}
	s, sock, err := a.session(ctx, h, e)
	if errors.Is(err, errNvimNotInPATH) {
		return nil, errors.Join(err, a.render(ctx, h, e, mainSlot, missingView()))
	}
	if err != nil {
		return nil, errors.Join(err, a.render(ctx, h, e, mainSlot, failedView(err)))
	}
	e.setSession(s)
	if err := a.render(ctx, h, e, mainSlot, runningView(s)); err != nil {
		return nil, err
	}
	c, err := connect(ctx, sock, func(ev nvimEvent) { a.nvimEvent(h, e, ev) })
	if err != nil {
		return nil, err
	}
	e.setClient(c)
	go a.applyTheme(c)
	go a.offerImport(h, e, c)
	return c, nil
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	e.life.Lock()
	defer e.life.Unlock()
	if !a.isCurrent(e) {
		return nil
	}
	if _, err := a.ensureLocked(ctx, h, e); err != nil {
		return err
	}
	a.scheduleStatus(h, e)
	return a.render(ctx, h, e, mainSlot, runningView(e.sessionID()))
}

func (a *app) restart(ctx context.Context, h *pluginsdk.Host, cur *editor, all, force bool) error {
	sessions, err := h.ListSessions(ctx, cur.id, true)
	if err != nil {
		return err
	}
	running := map[*editor]bool{}
	var kill []string
	for _, s := range sessions {
		if s.State != stateRunning {
			continue
		}
		_, owned := ownedSocket(s, cur.id, s.Cwd)
		if !all && (!owned || s.Cwd != cur.root) {
			continue
		}
		kill = append(kill, s.ID)
		if !owned {
			continue
		}
		e := a.editorAt(cur.id, s.Cwd)
		if e == nil {
			return errDeleted
		}
		running[e] = true
	}
	editors := slices.Collect(maps.Keys(running))
	if !running[cur] {
		editors = append(editors, cur)
	}
	defer lockAll(editors)()
	if !force {
		if err := a.checkUnsaved(ctx, h, editors, running); err != nil {
			return err
		}
	}
	for _, e := range editors {
		if c := e.reset(); c != nil {
			c.close()
		}
	}
	a.scheduleStatus(h, cur)
	for _, id := range kill {
		if err := h.KillSession(ctx, id); err != nil {
			return err
		}
	}
	var errs []error
	for _, e := range editors {
		if e == cur || running[e] {
			_, err := a.ensureLocked(ctx, h, e)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *app) stopRoot(ctx context.Context, h *pluginsdk.Host, projectID, root string, force bool) error {
	sessions, err := h.ListSessions(ctx, projectID, true)
	if err != nil {
		return err
	}
	var kill []string
	for _, s := range sessions {
		if _, ok := ownedSocket(s, projectID, root); ok {
			kill = append(kill, s.ID)
		}
	}
	e := a.lookup(projectID, root)
	if e == nil && len(kill) == 0 {
		a.setStopping(projectID, root, true)
		return nil
	}
	if e == nil {
		if e = a.editorAt(projectID, root); e == nil {
			return nil
		}
	}
	e.life.Lock()
	defer e.life.Unlock()
	if !force {
		if err := a.checkUnsaved(ctx, h, []*editor{e}, map[*editor]bool{e: len(kill) > 0}); err != nil {
			return err
		}
	}
	a.setStopping(projectID, root, true)
	if c := e.reset(); c != nil {
		c.close()
	}
	a.forget(e)
	for _, id := range kill {
		if err := h.KillSession(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) releaseRoot(ctx context.Context, h *pluginsdk.Host, projectID, root string) error {
	a.setStopping(projectID, root, false)
	e, err := a.active(ctx, h, projectID)
	if e == nil || err != nil || e.root != root {
		return err
	}
	return a.show(ctx, h, e)
}

func (a *app) unsaved(projectID, root string) ([]string, error) {
	dir := gitrepo.Canonical(root)
	var inside []string
	for _, e := range a.projectEditors(projectID) {
		c := e.connected()
		if c == nil {
			continue
		}
		modified, err := c.modified()
		if err != nil {
			return nil, errUnsavedUnknown
		}
		own := gitrepo.Canonical(e.root)
		for _, name := range modified {
			if name == "" {
				continue
			}
			file := gitrepo.Canonical(name)
			if gitrepo.Within(file, dir) && (own == dir || !gitrepo.Within(file, own)) {
				inside = append(inside, name)
			}
		}
	}
	slices.Sort(inside)
	return relativeNames(slices.Compact(inside), a.projectFolders(projectID)), nil
}

func (a *app) exited(ctx context.Context, h *pluginsdk.Host, ev sessionExited) {
	for _, e := range a.projectEditors(ev.ProjectID) {
		c, ok := e.exited(ev.ID)
		if !ok {
			continue
		}
		if c != nil {
			go c.close()
		}
		a.scheduleStatus(h, e)
		if err := a.render(ctx, h, e, mainSlot, exitedView(ev.ExitCode)); err != nil {
			logError("render", err)
		}
		return
	}
}

func (a *app) session(ctx context.Context, h *pluginsdk.Host, e *editor) (string, string, error) {
	sessions, err := h.ListSessions(ctx, e.id, true)
	if err != nil {
		return "", "", err
	}
	for _, s := range sessions {
		if sock, ok := ownedSocket(s, e.id, e.root); ok {
			return s.ID, sock, nil
		}
	}
	sock, err := socketPath(pluginsdk.RuntimeDir(), e.id, e.root)
	if err != nil {
		return "", "", err
	}
	l, err := nvimLaunch()
	if err != nil {
		return "", "", err
	}
	if err := a.render(ctx, h, e, mainSlot, startingView()); err != nil {
		return "", "", err
	}
	for _, s := range sessions {
		if s.State != stateRunning {
			if err := h.RemoveSession(ctx, s.ID); err != nil {
				return "", "", err
			}
		}
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("remove stale socket: %w", err)
	}
	s, err := h.CreateSession(ctx, pluginsdk.SessionRequest{
		ProjectID:  e.id,
		Title:      sessionTitle,
		Command:    append(append([]string{l.Path}, l.Args...), "--listen", sock),
		Cwd:        e.root,
		Env:        l.Env,
		UnsetEnv:   l.Unset,
		PathAppend: l.PathAppend,
	})
	return s.ID, sock, err
}

func (a *app) checkUnsaved(ctx context.Context, h *pluginsdk.Host, editors []*editor, running map[*editor]bool) error {
	var names []string
	for _, e := range editors {
		c := e.connected()
		if c == nil {
			if !running[e] {
				continue
			}
			var err error
			if c, err = a.ensureLocked(ctx, h, e); err != nil {
				return errUnsavedUnknown
			}
		}
		modified, err := c.modified()
		if err != nil {
			return errUnsavedUnknown
		}
		names = append(names, relativeNames(modified, a.projectFolders(e.id))...)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %s", errUnsaved, strings.Join(names, ", "))
	}
	return nil
}

func ownedSocket(s pluginsdk.Session, projectID, root string) (string, bool) {
	n := len(s.Command)
	if s.State != stateRunning || s.Cwd != root || n < 3 || s.Command[n-2] != "--listen" {
		return "", false
	}
	sock := s.Command[n-1]
	rt := pluginsdk.RuntimeDir()
	want, err := socketPath(rt, projectID, root)
	return sock, err == nil && sock == want || sock == legacySocketPath(rt, projectID)
}

func relativeNames(names, folders []string) []string {
	shown := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			n = noName
		}
		shown = append(shown, relative(n, folders))
	}
	return shown
}

func quiet(err error) bool {
	return errors.Is(err, errNvimNotInPATH) || errors.Is(err, errDeleted) || errors.Is(err, errStopping)
}
