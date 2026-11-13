package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const noName = "[No Name]"

var (
	errUnsaved        = errors.New("unsaved buffers")
	errUnsavedUnknown = fmt.Errorf("%w: unknown (nvim not responding)", errUnsaved)
	errDeleted        = errors.New("workspace deleted")
	errStopping       = errors.New("the worktree is being removed")
	errEditorBusy     = errors.New("the editor is busy, try again")
	errUnsavedSwitch  = errors.New("Save or discard unsaved changes first")
)

func (a *app) ensureLogged(ctx context.Context, h *pluginsdk.Host, workspaceID string) {
	if a.isDeleted(workspaceID) {
		return
	}
	a.syncReviews(ctx, h, workspaceID)
	e, err := a.active(ctx, h, workspaceID)
	if e != nil && err == nil {
		_, err = a.ensure(ctx, h, e)
	}
	if err != nil && !quiet(err) {
		logError("start "+workspaceID, err)
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
	go a.adoptReviews(context.Background(), h, e.id, e.root, c)
	go a.applyTheme(c)
	go a.offerImport(h, e, c)
	return c, nil
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	if err := e.life.LockCtx(ctx); err != nil {
		return err
	}
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
		if s.State != contract.SessionStateRunning {
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

func (a *app) stopRoot(ctx context.Context, h *pluginsdk.Host, workspaceID, root string, force bool) error {
	if !force {
		names, err := a.unsaved(workspaceID, root)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			return fmt.Errorf("%w: %s", errUnsaved, strings.Join(names, ", "))
		}
	}
	sessions, err := h.ListSessions(ctx, workspaceID, true)
	if err != nil {
		return err
	}
	var kill []string
	for _, s := range sessions {
		if _, ok := ownedSocket(s, workspaceID, root); ok {
			kill = append(kill, s.ID)
		}
	}
	e := a.lookup(workspaceID, root)
	if e == nil && len(kill) == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", errEditorBusy, err)
		}
		a.setStopping(workspaceID, root, true)
		return nil
	}
	if e == nil {
		if e = a.editorAt(workspaceID, root); e == nil {
			return nil
		}
	}
	return a.stopEditor(ctx, h, e, kill, force)
}

func (a *app) stopEditor(ctx context.Context, h *pluginsdk.Host, e *editor, kill []string, force bool) error {
	if err := e.life.LockCtx(ctx); err != nil {
		return fmt.Errorf("%w: %w", errEditorBusy, err)
	}
	defer e.life.Unlock()
	if !force {
		if err := a.checkUnsaved(ctx, h, []*editor{e}, map[*editor]bool{e: len(kill) > 0}); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errEditorBusy, err)
	}
	a.setStopping(e.id, e.root, true)
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

func (a *app) releaseRoot(ctx context.Context, h *pluginsdk.Host, workspaceID, root string) error {
	a.setStopping(workspaceID, root, false)
	e, err := a.active(ctx, h, workspaceID)
	if e == nil || err != nil || e.root != root {
		return err
	}
	return a.show(ctx, h, e)
}

func (a *app) guardSwitch(workspaceID, root string) error {
	names, err := a.unsaved(workspaceID, root)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnsavedSwitch, err)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %s", errUnsavedSwitch, strings.Join(names, ", "))
	}
	return nil
}

func (a *app) unsaved(workspaceID, root string) ([]string, error) {
	dir := gitrepo.Canonical(root)
	var inside []string
	for _, e := range a.workspaceEditors(workspaceID) {
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
			if gitrepo.Within(file, dir) && (own == dir || !gitrepo.Within(file, own) || !gitrepo.Within(own, dir)) {
				inside = append(inside, name)
			}
		}
	}
	slices.Sort(inside)
	return relativeNames(slices.Compact(inside), a.workspaceFolders(workspaceID)), nil
}

func (a *app) exited(ctx context.Context, h *pluginsdk.Host, ev contract.SessionExited) {
	for _, e := range a.workspaceEditors(ev.Workspace) {
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
		if s.State != contract.SessionStateRunning {
			if err := h.RemoveSession(ctx, s.ID); err != nil {
				return "", "", err
			}
		}
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("remove stale socket: %w", err)
	}
	s, err := h.CreateSession(ctx, pluginsdk.SessionRequest{
		WorkspaceID: e.id,
		Title:       sessionTitle,
		Command:     append(append([]string{l.Path}, l.Args...), "--listen", sock),
		Cwd:         e.root,
		Env:         l.Env,
		UnsetEnv:    l.Unset,
		PathAppend:  l.PathAppend,
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
		names = append(names, relativeNames(modified, a.workspaceFolders(e.id))...)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %s", errUnsaved, strings.Join(names, ", "))
	}
	return nil
}

func ownedSocket(s pluginsdk.Session, workspaceID, root string) (string, bool) {
	n := len(s.Command)
	if s.State != contract.SessionStateRunning || s.Cwd != root || n < 3 || s.Command[n-2] != "--listen" {
		return "", false
	}
	sock := s.Command[n-1]
	rt := pluginsdk.RuntimeDir()
	want, err := socketPath(rt, workspaceID, root)
	return sock, err == nil && sock == want || sock == legacySocketPath(rt, workspaceID)
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
