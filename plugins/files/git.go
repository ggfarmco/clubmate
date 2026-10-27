package main

import (
	"context"
	"os"
	"path/filepath"

	"clubmate/plugins/internal/gitrepo"
)

func statusMap(entries []gitrepo.Entry) map[string]string {
	status := map[string]string{}
	for _, e := range entries {
		if code := statusCode(e.X, e.Y); code != "" {
			status[e.Path] = code
		}
	}
	return status
}

func statusCode(x, y byte) string {
	switch {
	case x == '?' && y == '?':
		return "U"
	case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
		return "U"
	case x == 'A':
		return "A"
	}
	c := y
	if c == ' ' {
		c = x
	}
	switch c {
	case 'M', 'T':
		return "M"
	case 'A', 'C':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	}
	return ""
}

func collectIgnored(ctx context.Context, repo string, expanded map[string]bool) (map[string]bool, error) {
	var paths []string
	var walk func(dir string)
	walk = func(dir string) {
		if !expanded[dir] || (dir != repo && nestedRepo(dir)) {
			return
		}
		for i, e := range readEntries(dir) {
			if i >= maxEntries || len(paths) >= maxItems {
				return
			}
			p := filepath.Join(dir, e.name)
			paths = append(paths, p)
			if e.dir && !e.link {
				walk(p)
			}
		}
	}
	walk(repo)
	return gitrepo.Ignored(ctx, repo, paths)
}

func nestedRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
