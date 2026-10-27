package main

import (
	"context"
	"maps"
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
	ignored := map[string]bool{}
	queue := []string{repo}
	total := 0
	for len(queue) > 0 && total < maxItems {
		dir := queue[0]
		queue = queue[1:]
		if !expanded[dir] {
			continue
		}
		entries := readEntries(dir)
		if len(entries) > maxEntries {
			entries = entries[:maxEntries]
		}
		paths := make([]string, len(entries))
		for i, e := range entries {
			paths[i] = filepath.Join(dir, e.name)
		}
		total += len(paths)
		found, err := gitrepo.Ignored(ctx, repo, paths)
		if err != nil {
			return nil, err
		}
		maps.Copy(ignored, found)
		for i, e := range entries {
			if e.dir && !e.link && !found[paths[i]] {
				queue = append(queue, paths[i])
			}
		}
	}
	return ignored, nil
}
