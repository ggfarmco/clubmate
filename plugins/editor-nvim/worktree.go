package main

import (
	"context"
	"path/filepath"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

type worktree struct {
	path string
	real string
}

func owner(ctx context.Context, p pluginsdk.Project, path string) (string, string, bool) {
	if !filepath.IsAbs(path) {
		return "", "", false
	}
	target := resolve(filepath.Clean(path))
	for i, repo := range p.Repos {
		top, ok := repoRoot(repo)
		if !ok {
			continue
		}
		trees, err := worktrees(ctx, top)
		if err != nil {
			logError("worktree list", err)
			continue
		}
		folder := resolve(repo)
		checkout, owning := longest(trees, folder), longest(trees, target)
		if checkout == nil || owning == nil {
			continue
		}
		rel, err := filepath.Rel(checkout.real, folder)
		if err != nil {
			continue
		}
		mapped := filepath.Join(owning.real, rel)
		if !within(target, mapped) {
			continue
		}
		if i < len(p.Roots) && resolve(p.Roots[i]) == mapped {
			return "", "", false
		}
		if mapped == folder {
			return repo, "", true
		}
		return repo, filepath.Join(owning.path, rel), true
	}
	return "", "", false
}

func worktrees(ctx context.Context, top string) ([]worktree, error) {
	out, err := gitrepo.Run(ctx, top, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var trees []worktree
	for field := range strings.SplitSeq(string(out), "\x00") {
		if tree, ok := strings.CutPrefix(field, "worktree "); ok {
			trees = append(trees, worktree{path: tree, real: resolve(tree)})
		}
	}
	return trees, nil
}

func longest(trees []worktree, path string) *worktree {
	var best *worktree
	for i, t := range trees {
		if within(path, t.real) && (best == nil || len(t.real) > len(best.real)) {
			best = &trees[i]
		}
	}
	return best
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func resolve(path string) string {
	rest := ""
	for dir := path; ; {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}
