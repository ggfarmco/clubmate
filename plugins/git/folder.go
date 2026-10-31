package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func gitFolder(ctx context.Context, h *pluginsdk.Host, projectID, repo string) (*folder, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.Home {
		return nil, errHomeWorktrees
	}
	for i, r := range p.Repos {
		if repo != "" && r != repo {
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

func eachFolder(ctx context.Context, h *pluginsdk.Host, projectID string, fn func(*folder) error) error {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if p.Home {
		return nil
	}
	for i := range p.Repos {
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

func (f *folder) shortHeads(ctx context.Context) (map[string]string, error) {
	heads := map[string]string{}
	for _, t := range f.Trees {
		if _, ok := heads[t.Head]; ok || t.Head == "" {
			continue
		}
		out, err := run(ctx, f.Checkout.Path, "rev-parse", "--short", t.Head)
		if err != nil {
			return nil, err
		}
		heads[t.Head] = strings.TrimSpace(string(out))
	}
	return heads, nil
}

func openFolder(ctx context.Context, p pluginsdk.Project, i int) (*folder, error) {
	f, err := gitrepo.OpenFolder(ctx, p.Repos[i])
	if err != nil {
		return nil, gitError(nil, err)
	}
	root := p.Repos[i]
	if i < len(p.Roots) {
		root = p.Roots[i]
	}
	return &folder{Folder: f, root: root}, nil
}
