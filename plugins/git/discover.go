package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

const (
	reposDepth   = 2
	reposWalk    = 3 * time.Second
	reposDated   = 10 * time.Second
	reposWorkers = 8
	nodeModules  = "node_modules"
)

type reposIn struct {
	Path    string      `json:"path"`
	Repo    bool        `json:"repo"`
	Repos   []foundRepo `json:"repos"`
	Partial bool        `json:"partial"`
}

type foundRepo struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	LastCommit time.Time `json:"lastCommit,omitzero"`
}

type lastCommitFunc func(ctx context.Context, top string) time.Time

func reposInCommand(ctx context.Context, _ *app, c call) (any, error) {
	if !filepath.IsAbs(c.args.Path) {
		return nil, fmt.Errorf("invalid_params: %w: %s", errRelativePath, c.args.Path)
	}
	return findReposIn(ctx, filepath.Clean(c.args.Path), time.Now().Add(reposWalk), c.args.Dates, lastCommit), nil
}

func findReposIn(ctx context.Context, root string, until time.Time, dated bool, last lastCommitFunc) reposIn {
	out := reposIn{Path: root, Repos: []foundRepo{}}
	if _, ok := gitrepo.RepoRoot(root); ok {
		out.Repo = true
		return out
	}
	out.Repos, out.Partial = walkRepos(ctx, root, until)
	if dated {
		out.Partial = dateRepos(ctx, out.Repos, last) || out.Partial
	}
	slices.SortStableFunc(out.Repos, func(x, y foundRepo) int {
		if c := y.LastCommit.Compare(x.LastCommit); c != 0 {
			return c
		}
		return cmp.Compare(x.Name, y.Name)
	})
	return out
}

func walkRepos(ctx context.Context, root string, until time.Time) ([]foundRepo, bool) {
	var out []foundRepo
	dirs := []string{root}
	for range reposDepth {
		var next []string
		for _, dir := range dirs {
			if ctx.Err() != nil || !time.Now().Before(until) {
				return out, true
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if name := e.Name(); !e.IsDir() || strings.HasPrefix(name, ".") || name == nodeModules {
					continue
				}
				path := filepath.Join(dir, e.Name())
				if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
					next = append(next, path)
					continue
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					continue
				}
				out = append(out, foundRepo{Path: path, Name: filepath.ToSlash(rel)})
			}
		}
		dirs = next
	}
	return out, false
}

func dateRepos(ctx context.Context, repos []foundRepo, last lastCommitFunc) bool {
	ctx, cancel := context.WithTimeout(ctx, reposDated)
	defer cancel()
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(reposWorkers, len(repos)) {
		wg.Go(func() {
			for i := range work {
				repos[i].LastCommit = last(ctx, repos[i].Path)
			}
		})
	}
	stopped := false
	for i := range repos {
		select {
		case work <- i:
		case <-ctx.Done():
			stopped = true
		}
		if stopped {
			break
		}
	}
	close(work)
	wg.Wait()
	return stopped || ctx.Err() != nil
}
