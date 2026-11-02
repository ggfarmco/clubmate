package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
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
	return a.targetWith(ctx, h, workspaceID, path, a.show)
}

func (a *app) targetWith(ctx context.Context, h *pluginsdk.Host, workspaceID, path string, show func(context.Context, *pluginsdk.Host, *editor) error) (*editor, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	folder, root, switched := owner(ctx, p, path)
	if switched {
		if p, err = h.SetWorktree(ctx, workspaceID, p.Folders[folder].Path, root); err != nil {
			return nil, err
		}
	}
	want := ""
	if folder >= 0 && folder < len(p.Folders) {
		want = p.Folders[folder].Root()
	}
	e, moved, err := a.selectRoot(p, want)
	if e == nil || err != nil {
		return e, err
	}
	a.watch(ctx, h, p)
	if !moved && !switched {
		return e, nil
	}
	if err := show(ctx, h, e); err != nil {
		return nil, err
	}
	if switched {
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
	if moved {
		a.setNvimShown(p.ID, false)
	}
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
	existing := existingRoots(p)
	fallback, err := firstRoot(existing)
	if err != nil {
		return nil, false, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[p.ID] {
		return nil, false, nil
	}
	prev, known := a.current[p.ID]
	root := fallback
	switch changed := changedRoot(a.workspaces[p.ID], p); {
	case slices.Contains(existing, want):
		root = want
	case slices.Contains(existing, changed):
		root = changed
	case known && slices.Contains(existing, prev):
		root = prev
	}
	a.current[p.ID] = root
	a.workspaces[p.ID] = p
	return a.editorLocked(p.ID, root), known && prev != root, nil
}

func (a *app) tracked(workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.current[workspaceID]
	return ok
}

func (a *app) workspaceFolders(workspaceID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.workspaces[workspaceID]
	return slices.Concat(p.Paths(), p.Roots())
}

func changedRoot(prev, p pluginsdk.Workspace) string {
	if !slices.Equal(prev.Paths(), p.Paths()) {
		return ""
	}
	changed := ""
	for i, f := range p.Folders {
		r := f.Root()
		if r == prev.Folders[i].Root() {
			continue
		}
		if changed != "" {
			return ""
		}
		changed = r
	}
	return changed
}

func existingRoots(p pluginsdk.Workspace) []string {
	var out []string
	for _, r := range p.Roots() {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

func firstRoot(existing []string) (string, error) {
	if len(existing) > 0 {
		return existing[0], nil
	}
	return os.UserHomeDir()
}

func owner(ctx context.Context, p pluginsdk.Workspace, path string) (int, string, bool) {
	if !filepath.IsAbs(path) {
		return -1, "", false
	}
	target := gitrepo.Canonical(filepath.Clean(path))
	for i, folder := range p.Folders {
		f, err := gitrepo.OpenFolder(ctx, folder.Path)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			continue
		}
		if err != nil {
			logError("worktree list", err)
			continue
		}
		owning := gitrepo.Longest(f.Trees, target)
		if owning == nil {
			continue
		}
		mapped := f.Mapped(*owning)
		if !gitrepo.Within(target, mapped) {
			continue
		}
		if gitrepo.Canonical(folder.Root()) == mapped {
			return i, "", false
		}
		return i, f.Selection(*owning), true
	}
	return containing(p.Roots(), target), "", false
}

func containing(roots []string, target string) int {
	found, longest := -1, -1
	for i, r := range roots {
		dir := gitrepo.Canonical(r)
		if gitrepo.Within(target, dir) && len(dir) > longest {
			found, longest = i, len(dir)
		}
	}
	return found
}
