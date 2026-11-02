package editorroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

type Roots struct {
	current    map[string]string
	workspaces map[string]pluginsdk.Workspace
	deleted    map[string]bool
}

type Target struct {
	Workspace pluginsdk.Workspace
	Want      string
	Switched  bool
}

func NewRoots() Roots {
	return Roots{current: map[string]string{}, workspaces: map[string]pluginsdk.Workspace{}, deleted: map[string]bool{}}
}

func (r Roots) Select(p pluginsdk.Workspace, existing []string, want string) (string, bool, bool, error) {
	if r.deleted[p.ID] {
		return "", false, false, nil
	}
	prev, known := r.current[p.ID]
	root := ""
	for _, c := range []string{want, changed(r.workspaces[p.ID], p), prev} {
		if c != "" && slices.Contains(existing, c) {
			root = c
			break
		}
	}
	if root == "" {
		if len(existing) > 0 {
			root = existing[0]
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", false, false, err
			}
			root = home
		}
	}
	r.current[p.ID] = root
	r.workspaces[p.ID] = p
	return root, known && prev != root, true, nil
}

func (r Roots) Current(workspaceID string) (string, bool) {
	if r.deleted[workspaceID] {
		return "", false
	}
	root, ok := r.current[workspaceID]
	return root, ok
}

func (r Roots) Workspace(workspaceID string) pluginsdk.Workspace {
	return r.workspaces[workspaceID]
}

func (r Roots) Tracked() []string {
	ids := make([]string, 0, len(r.current))
	for id := range r.current {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (r Roots) Drop(workspaceID string) {
	delete(r.current, workspaceID)
	delete(r.workspaces, workspaceID)
	r.deleted[workspaceID] = true
}

func (r Roots) Deleted(workspaceID string) bool {
	return r.deleted[workspaceID]
}

func Existing(p pluginsdk.Workspace) []string {
	var out []string
	for _, r := range p.Roots() {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

func Resolve(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (Target, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return Target{}, err
	}
	folder, root, switched := owner(ctx, p, path)
	if switched {
		if p, err = h.SetWorktree(ctx, workspaceID, p.Folders[folder].Path, root); err != nil {
			return Target{}, err
		}
	}
	t := Target{Workspace: p, Switched: switched}
	if folder >= 0 && folder < len(p.Folders) {
		t.Want = p.Folders[folder].Root()
	}
	return t, nil
}

func changed(prev, p pluginsdk.Workspace) string {
	if !slices.Equal(prev.Paths(), p.Paths()) {
		return ""
	}
	found := ""
	for i, f := range p.Folders {
		r := f.Root()
		if r == prev.Folders[i].Root() {
			continue
		}
		if found != "" {
			return ""
		}
		found = r
	}
	return found
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
			fmt.Fprintln(os.Stderr, "worktree list:", err)
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
