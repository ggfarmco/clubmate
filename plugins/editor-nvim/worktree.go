package main

import (
	"context"
	"errors"
	"path/filepath"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

func owner(ctx context.Context, p pluginsdk.Project, path string) (string, string, bool) {
	if !filepath.IsAbs(path) {
		return "", "", false
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
			return "", "", false
		}
		return repo, f.Selection(*owning), true
	}
	return "", "", false
}
