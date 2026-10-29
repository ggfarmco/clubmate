package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

func owner(ctx context.Context, p pluginsdk.Project, path string) (string, string, bool) {
	if !filepath.IsAbs(path) {
		return "", "", false
	}
	path = resolve(filepath.Clean(path))
	top, ok := repoRoot(nearestDir(path))
	if !ok {
		return "", "", false
	}
	trees, err := worktrees(ctx, top)
	if err != nil {
		logError("worktree list", err)
		return "", "", false
	}
	owning := longest(trees, path)
	if owning == "" {
		return "", "", false
	}
	for i, repo := range p.Repos {
		folder := resolve(repo)
		checkout := longest(trees, folder)
		if checkout == "" {
			continue
		}
		rel, err := filepath.Rel(checkout, folder)
		if err != nil {
			continue
		}
		mapped := filepath.Join(owning, rel)
		if !within(path, mapped) {
			continue
		}
		if i < len(p.Roots) && resolve(p.Roots[i]) == mapped {
			return "", "", false
		}
		if mapped == folder {
			mapped = ""
		}
		return repo, mapped, true
	}
	return "", "", false
}

func worktrees(ctx context.Context, top string) ([]string, error) {
	out, err := gitrepo.Run(ctx, top, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var trees []string
	for field := range strings.SplitSeq(string(out), "\x00") {
		if tree, ok := strings.CutPrefix(field, "worktree "); ok {
			trees = append(trees, resolve(tree))
		}
	}
	return trees, nil
}

func longest(trees []string, path string) string {
	best := ""
	for _, t := range trees {
		if within(path, t) && len(t) > len(best) {
			best = t
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

func nearestDir(path string) string {
	for {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
