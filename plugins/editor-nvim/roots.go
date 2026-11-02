package main

import (
	"context"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
)

func (a *app) active(ctx context.Context, h *pluginsdk.Host, workspaceID string) (*editor, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	e, moved, err := a.selectRoot(p, "")
	if e == nil || err != nil {
		return e, err
	}
	a.watch(ctx, h, p)
	if !moved {
		return e, nil
	}
	return e, a.show(ctx, h, e)
}

func (a *app) target(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (*editor, error) {
	t, err := editorroot.Resolve(ctx, h, workspaceID, path)
	if err != nil {
		return nil, err
	}
	e, moved, err := a.selectRoot(t.Workspace, t.Want)
	if e == nil || err != nil {
		return e, err
	}
	a.watch(ctx, h, t.Workspace)
	if !moved && !t.Switched {
		return e, nil
	}
	if err := a.show(ctx, h, e); err != nil {
		return nil, err
	}
	if t.Switched {
		focusMain(ctx, h, workspaceID)
	}
	return e, nil
}

func (a *app) workspaceUpdated(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Workspace) {
	if !a.tracked(p.ID) {
		return
	}
	e, moved, err := a.selectRoot(p, "")
	if err != nil {
		logError("workspace.updated", err)
		return
	}
	if e == nil {
		return
	}
	a.watch(ctx, h, p)
	a.scheduleStatus(h, e)
	if moved {
		go func() {
			if err := a.show(ctx, h, e); err != nil && !quiet(err) {
				logError("switch "+p.ID, err)
			}
		}()
	}
}

func (a *app) selectRoot(p pluginsdk.Workspace, want string) (*editor, bool, error) {
	existing := editorroot.Existing(p)
	a.mu.Lock()
	defer a.mu.Unlock()
	root, moved, ok, err := a.roots.Select(p, existing, want)
	if !ok || err != nil {
		return nil, false, err
	}
	return a.editorLocked(p.ID, root), moved, nil
}

func (a *app) tracked(workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.roots.Current(workspaceID)
	return ok
}

func (a *app) workspaceFolders(workspaceID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.roots.Workspace(workspaceID)
	return slices.Concat(p.Paths(), p.Roots())
}
