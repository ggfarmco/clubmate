package rpc

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/fswatch"
)

const (
	notifyFSChanged = "fs.changed"

	kindFS = "fs"
)

type fsChange struct {
	paths    []string
	overflow bool
}

type fsChangedParams struct {
	Watch     string   `json:"watch"`
	ProjectID string   `json:"projectId"`
	Paths     []string `json:"paths"`
	Overflow  bool     `json:"overflow"`
}

type watchParams struct {
	Watch string `json:"watch"`
}

func (s *Server) fsMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"fs.watch":   method(s.fsWatch),
		"fs.unwatch": method(s.fsUnwatch),
	}
}

func (s *Server) fsWatch(ctx context.Context, p projectParams) (watchParams, error) {
	if err := requireID(projectPrefix, p.ProjectID); err != nil {
		return watchParams{}, err
	}
	proj, err := s.svc.GetProject(ctx, p.ProjectID)
	if err != nil {
		return watchParams{}, err
	}
	roots := make([]string, 0, len(proj.Repos))
	for _, repo := range proj.Repos {
		root, err := filepath.EvalSymlinks(repo)
		if err != nil {
			s.logger.Warn("fs.watch skips unresolvable repo", "repo", repo, "err", err)
			continue
		}
		roots = append(roots, root)
	}
	changes := make(chan fsChange)
	quit := make(chan struct{})
	stop := func() {}
	if len(roots) > 0 {
		stop, err = fswatch.Watch(roots, func(paths []string, overflow bool) {
			select {
			case changes <- fsChange{paths: paths, overflow: overflow}:
			case <-quit:
			}
		})
		if err != nil {
			return watchParams{}, fmt.Errorf("watch project %s: %w", p.ProjectID, err)
		}
	}
	sid, err := subscriptionsFrom(ctx).add(kindFS, sync.OnceFunc(func() {
		close(quit)
		stop()
	}), forwardFS(ctx, connFrom(ctx), p.ProjectID, changes))
	return watchParams{Watch: sid}, err
}

func (s *Server) fsUnwatch(ctx context.Context, p watchParams) (empty, error) {
	return empty{}, subscriptionsFrom(ctx).cancel(kindFS, p.Watch)
}

func forwardFS(ctx context.Context, conn *jsonrpc2.Conn, projectID string, changes <-chan fsChange) forwardFunc {
	return func(sid string, done <-chan struct{}) {
		for {
			select {
			case <-done:
				return
			case c := <-changes:
				params := fsChangedParams{Watch: sid, ProjectID: projectID, Paths: nonNil(c.paths), Overflow: c.overflow}
				if err := conn.Notify(ctx, notifyFSChanged, params); err != nil {
					return
				}
			}
		}
	}
}
