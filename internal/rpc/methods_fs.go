package rpc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	Watch       string   `json:"watch"`
	WorkspaceID string   `json:"workspaceId"`
	Paths       []string `json:"paths"`
	Overflow    bool     `json:"overflow"`
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

func (s *Server) fsWatch(ctx context.Context, p workspaceParams) (watchParams, error) {
	if err := requireID(workspacePrefix, p.WorkspaceID); err != nil {
		return watchParams{}, err
	}
	ws, err := s.svc.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return watchParams{}, err
	}
	folders := slices.Compact(slices.Sorted(slices.Values(slices.Concat(ws.Paths(), ws.Roots()))))
	roots := make([]string, 0, len(folders))
	var gitRoots []string
	for _, repo := range folders {
		root, err := filepath.EvalSymlinks(repo)
		if err != nil {
			s.logger.Warn("fs.watch skips unresolvable repo", "repo", repo, "err", err)
			continue
		}
		roots = append(roots, root)
		linked := worktreeRoots(root)
		roots = append(roots, linked...)
		gitRoots = append(gitRoots, linked...)
	}
	roots = slices.Compact(slices.Sorted(slices.Values(roots)))
	gitRoots = slices.Compact(slices.Sorted(slices.Values(gitRoots)))
	changes := make(chan fsChange)
	quit := make(chan struct{})
	stop := func() {}
	if len(roots) > 0 {
		stop, err = fswatch.Watch(roots, gitRoots, func(paths []string, overflow bool) {
			select {
			case changes <- fsChange{paths: paths, overflow: overflow}:
			case <-quit:
			}
		})
		if err != nil {
			return watchParams{}, fmt.Errorf("watch workspace %s: %w", p.WorkspaceID, err)
		}
	}
	sid, err := subscriptionsFrom(ctx).add(kindFS, sync.OnceFunc(func() {
		close(quit)
		stop()
	}), forwardFS(ctx, connFrom(ctx), p.WorkspaceID, changes))
	return watchParams{Watch: sid}, err
}

func worktreeRoots(repo string) []string {
	data, err := os.ReadFile(filepath.Join(repo, ".git"))
	if err != nil {
		return nil
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return nil
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(repo, target)
	}
	gitdir, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil
	}
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return []string{gitdir}
	}
	commonDir := strings.TrimSpace(string(common))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitdir, commonDir)
	}
	resolved, err := filepath.EvalSymlinks(commonDir)
	if err != nil {
		return []string{gitdir}
	}
	return []string{resolved}
}

func (s *Server) fsUnwatch(ctx context.Context, p watchParams) (empty, error) {
	return empty{}, subscriptionsFrom(ctx).cancel(kindFS, p.Watch)
}

func forwardFS(ctx context.Context, conn *jsonrpc2.Conn, workspaceID string, changes <-chan fsChange) forwardFunc {
	return func(sid string, done <-chan struct{}) {
		for {
			select {
			case <-done:
				return
			case c := <-changes:
				params := fsChangedParams{Watch: sid, WorkspaceID: workspaceID, Paths: nonNil(c.paths), Overflow: c.overflow}
				if err := conn.Notify(ctx, notifyFSChanged, params); err != nil {
					return
				}
			}
		}
	}
}
