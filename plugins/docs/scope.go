package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"clubmate/pkg/contract"
	"clubmate/pkg/docext"
	"clubmate/pkg/docrender"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/gitrepo"
)

const maxDoc = 4 << 20

var (
	errNotInWorkspace = errors.New("not in workspace")
	errNotDocument    = errors.New("not a document")
	errTooLarge       = errors.New("too large")
	errNotFound       = errors.New("not found")
)

func allowed(ws contract.Workspace, worktrees map[string][]gitrepo.Worktree, uploads []string) func(string) bool {
	var roots []string
	for _, f := range ws.Folders {
		roots = append(roots, gitrepo.Canonical(f.Path), gitrepo.Canonical(f.Root()))
		roots = append(roots, mapped(gitrepo.Canonical(f.Path), worktrees[f.Path])...)
	}
	for _, dir := range uploads {
		roots = append(roots, gitrepo.Canonical(dir))
	}
	return func(path string) bool {
		path = gitrepo.Canonical(filepath.Clean(path))
		for _, root := range roots {
			if gitrepo.Within(path, root) {
				return true
			}
		}
		return false
	}
}

func load(path string, allow func(string) bool) (string, []byte, error) {
	abs := gitrepo.Canonical(filepath.Clean(path))
	if !allow(abs) {
		return abs, nil, errNotInWorkspace
	}
	if !viewable(abs) {
		return abs, nil, errNotDocument
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return abs, nil, errNotFound
	}
	if err != nil {
		return abs, nil, err
	}
	if info.IsDir() {
		return abs, nil, errNotDocument
	}
	if info.Size() > maxDoc {
		return abs, nil, errTooLarge
	}
	src, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return abs, nil, errNotFound
	}
	return abs, src, err
}

func viewable(path string) bool {
	return docext.Is(path) || docext.IsArtifact(path)
}

func render(src []byte, path string, allow func(string) bool) (docrender.Doc, string) {
	if docext.IsArtifact(path) {
		return docrender.Artifact(src, path), sdui.DocumentArtifact
	}
	return docrender.Render(src, docrender.Options{Path: path, Allowed: allow}), sdui.DocumentMarkdown
}

func (d *docs) scope(ctx context.Context, h *pluginsdk.Host, ws contract.Workspace) func(string) bool {
	return workspaceScope(ctx, ws, d.sessionUploads(ctx, h, ws.ID))
}

func (d *docs) sessionUploads(ctx context.Context, h *pluginsdk.Host, workspaceID string) []string {
	if d.uploads == "" {
		return nil
	}
	sessions, err := agentcall.Sessions(ctx, h, workspaceID)
	if err != nil {
		logError("upload scope "+workspaceID, err)
		return nil
	}
	dirs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		if s.ID != "" && filepath.Base(s.ID) == s.ID {
			dirs = append(dirs, filepath.Join(d.uploads, s.ID))
		}
	}
	return dirs
}

func workspaceScope(ctx context.Context, ws contract.Workspace, uploads []string) func(string) bool {
	trees := make(map[string][]gitrepo.Worktree, len(ws.Folders))
	for _, f := range ws.Folders {
		top, ok := gitrepo.RepoRoot(gitrepo.Canonical(f.Path))
		if !ok {
			continue
		}
		if list, err := gitrepo.Worktrees(ctx, top); err == nil {
			trees[f.Path] = list
		}
	}
	return allowed(ws, trees, uploads)
}

func mapped(folder string, trees []gitrepo.Worktree) []string {
	checkout := gitrepo.Longest(trees, folder)
	if checkout == nil {
		return nil
	}
	rel, err := filepath.Rel(checkout.Real, folder)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(trees))
	for _, t := range trees {
		if !t.Bare {
			out = append(out, filepath.Join(t.Real, rel))
		}
	}
	return out
}
