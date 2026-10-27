package main

import (
	"context"
	"maps"

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
	case x == '!':
		return ignoredCode
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

func gitStatusAll(ctx context.Context, repos []string) map[string]string {
	all := map[string]string{}
	for _, repo := range repos {
		_, entries, err := gitrepo.Status(ctx, repo, true)
		if err != nil {
			continue
		}
		maps.Copy(all, statusMap(entries))
	}
	return all
}
