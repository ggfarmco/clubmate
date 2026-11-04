package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

var (
	errNoGitFolder   = errors.New("no git folder in the workspace")
	errUnknownRepo   = errors.New("unknown repository")
	errHomeWorktrees = errors.New("worktrees unavailable in the home workspace")
)

type folder struct {
	*gitrepo.Folder
	root string
}

type head struct {
	short     string
	committed time.Time
}

func gitFolder(ctx context.Context, h *pluginsdk.Host, workspaceID, repo string) (*folder, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if p.Home {
		return nil, errHomeWorktrees
	}
	for i, r := range p.Folders {
		if repo != "" && r.Path != repo {
			continue
		}
		f, err := openFolder(ctx, p, i)
		if repo != "" || err == nil {
			return f, err
		}
		if !errors.Is(err, gitrepo.ErrNotRepo) {
			return nil, err
		}
	}
	if repo != "" {
		return nil, fmt.Errorf("%w: %q", errUnknownRepo, repo)
	}
	return nil, errNoGitFolder
}

func eachFolder(ctx context.Context, h *pluginsdk.Host, workspaceID string, fn func(*folder) error) error {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if p.Home {
		return nil
	}
	for i := range p.Folders {
		f, err := openFolder(ctx, p, i)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			continue
		}
		if err != nil {
			return err
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

func (f *folder) member(path string) (gitrepo.Worktree, error) {
	if t, ok := f.Member(path); ok {
		return t, nil
	}
	return gitrepo.Worktree{}, fmt.Errorf("not a worktree of %s", f.Repo)
}

func (f *folder) current(t gitrepo.Worktree) bool {
	return gitrepo.Canonical(f.root) == f.Mapped(t)
}

func (f *folder) currentTree() gitrepo.Worktree {
	if t := gitrepo.Longest(f.Trees, gitrepo.Canonical(f.root)); t != nil {
		return *t
	}
	return f.Checkout
}

func (f *folder) treeName(ctx context.Context, t gitrepo.Worktree) string {
	if t.Branch != "" || t.Head == "" {
		return t.Branch
	}
	out, err := run(ctx, f.Checkout.Path, "rev-parse", "--short", t.Head)
	if err != nil {
		return t.Head
	}
	return strings.TrimSpace(string(out))
}

func (f *folder) heads(ctx context.Context) (map[string]head, error) {
	heads := map[string]head{}
	var shas []string
	for _, t := range f.Trees {
		if t.Head != "" && !slices.Contains(shas, t.Head) {
			shas = append(shas, t.Head)
		}
	}
	if len(shas) == 0 {
		return heads, nil
	}
	args := append(append([]string{"show", "-s", "--format=%H %h %cI"}, shas...), "--")
	out, err := run(ctx, f.Checkout.Path, args...)
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(string(out)) {
		full, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		short, date, _ := strings.Cut(rest, " ")
		committed, err := time.Parse(time.RFC3339, date)
		if err != nil {
			return nil, fmt.Errorf("commit date of %s: %w", short, err)
		}
		heads[full] = head{short: short, committed: committed}
	}
	return heads, nil
}

func openFolder(ctx context.Context, p pluginsdk.Workspace, i int) (*folder, error) {
	f, err := gitrepo.OpenFolder(ctx, p.Folders[i].Path)
	if err != nil {
		return nil, gitError(nil, err)
	}
	return &folder{Folder: f, root: p.Folders[i].Root()}, nil
}
