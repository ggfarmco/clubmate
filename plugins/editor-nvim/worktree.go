package main

import (
	"context"
	"errors"
	"path/filepath"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

func owner(ctx context.Context, p pluginsdk.Project, path string) (int, string, bool) {
	if !filepath.IsAbs(path) {
		return -1, "", false
	}
	target := gitrepo.Canonical(filepath.Clean(path))
	for i, repo := range p.Repos {
		f, err := gitrepo.OpenFolder(ctx, repo)
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
		if i < len(p.Roots) && gitrepo.Canonical(p.Roots[i]) == mapped {
			return i, "", false
		}
		return i, f.Selection(*owning), true
	}
	return containing(p.Roots, target), "", false
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
