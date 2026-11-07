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
	e, moved, err := a.selectRoot(ctx, h, p, "")
	if e == nil || err != nil {
		return e, err
	}
	a.watch(ctx, h, p)
	if !moved {
		return e, nil
	}
	return e, a.show(ctx, h, e)
}

func (a *app) codeRoot(ctx context.Context, h *pluginsdk.Host, workspaceID string, args editorroot.CodeRootArgs) (editorroot.CodeRoot, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return editorroot.CodeRoot{}, err
	}
	if args.Folder == "" {
		return editorroot.Describe(p, a.currentRoot(p)), nil
	}
	want, err := editorroot.Want(p, args.Folder)
	if err != nil {
		return editorroot.CodeRoot{}, err
	}
	e, moved, err := a.selectRoot(ctx, h, p, want)
	if e == nil || err != nil {
		return editorroot.CodeRoot{}, err
	}
	a.watch(ctx, h, p)
	if moved {
		if err := a.show(ctx, h, e); err != nil {
			return editorroot.CodeRoot{}, err
		}
	}
	return editorroot.Describe(p, e.root), nil
}

func (a *app) currentRoot(p pluginsdk.Workspace) string {
	a.mu.Lock()
	root, ok := a.roots.Current(p.ID)
	a.mu.Unlock()
	if existing := editorroot.Existing(p); !ok && len(existing) > 0 {
		return existing[0]
	}
	return root
}

func (a *app) target(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (*editor, error) {
	t, err := editorroot.Resolve(ctx, h, workspaceID, path)
	if err != nil {
		return nil, err
	}
	e, moved, err := a.selectRoot(ctx, h, t.Workspace, t.Want)
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
	e, moved, err := a.selectRoot(ctx, h, p, "")
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

func (a *app) selectRoot(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Workspace, want string) (*editor, bool, error) {
	existing := editorroot.Existing(p)
	a.mu.Lock()
	root, moved, ok, err := a.roots.Select(p, existing, want)
	if !ok || err != nil {
		a.mu.Unlock()
		return nil, false, err
	}
	e := a.editorLocked(p.ID, root)
	a.mu.Unlock()
	if moved {
		if err := editorroot.PublishMoved(ctx, h, p, root); err != nil {
			logError("publish root", err)
		}
	}
	return e, moved, nil
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
