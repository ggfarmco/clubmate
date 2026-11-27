package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"clubmate/pkg/contract"
	"clubmate/pkg/docext"
	"clubmate/pkg/docrender"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/wikiroot"
)

const (
	maxDoc       = 4 << 20
	worktreesDir = ".worktrees"
)

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
	if !info.Mode().IsRegular() {
		return abs, nil, errNotDocument
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return abs, nil, errNotFound
	}
	if err != nil {
		return abs, nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return abs, nil, err
	}
	if !info.Mode().IsRegular() {
		return abs, nil, errNotDocument
	}
	if info.Size() > maxDoc {
		return abs, nil, errTooLarge
	}
	src, err := io.ReadAll(io.LimitReader(f, maxDoc+1))
	if err != nil {
		return abs, nil, err
	}
	if len(src) > maxDoc {
		return abs, nil, errTooLarge
	}
	return abs, src, nil
}

type sessionsFn func() ([]api.Session, error)

func lazySessions(ctx context.Context, h *pluginsdk.Host, workspaceID string) sessionsFn {
	return sync.OnceValues(func() ([]api.Session, error) { return agentcall.Sessions(ctx, h, workspaceID) })
}

func (d *docs) landed(ws contract.Workspace, sessions sessionsFn, path string) string {
	sep := string(filepath.Separator)
	if !strings.Contains(path, sep+worktreesDir+sep) {
		return path
	}
	if _, err := os.Lstat(path); err == nil {
		return path
	}
	list, err := sessions()
	if err != nil {
		logError("landed copy of "+path, err)
		return path
	}
	path = gitrepo.Canonical(path)
	tree := ""
	for _, s := range list {
		if t := gitrepo.Canonical(s.Worktree); s.Worktree != "" && gitrepo.Within(path, t) && len(t) > len(tree) {
			tree = t
		}
	}
	rel, err := filepath.Rel(tree, path)
	if tree == "" || err != nil {
		return path
	}
	for _, f := range ws.Folders {
		folder := gitrepo.Canonical(f.Path)
		repo, ok := gitrepo.RepoRoot(folder)
		if !ok {
			continue
		}
		sub, err := filepath.Rel(repo, folder)
		if err != nil {
			continue
		}
		top := tree
		if sub != "." {
			if top, ok = strings.CutSuffix(tree, sep+sub); !ok {
				continue
			}
		}
		if !strings.HasPrefix(top, filepath.Join(repo, worktreesDir)+sep) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(top, ".git")); err == nil {
			return path
		}
		if file := filepath.Join(folder, rel); isFile(file) {
			return file
		}
	}
	return path
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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

func (d *docs) scope(ctx context.Context, ws contract.Workspace, sessions sessionsFn) func(string) bool {
	return workspaceScope(ctx, ws, d.sessionUploads(ws.ID, sessions))
}

func (d *docs) sessionUploads(workspaceID string, sessions sessionsFn) []string {
	if d.uploads == "" {
		return nil
	}
	list, err := sessions()
	if err != nil {
		logError("upload scope "+workspaceID, err)
		return nil
	}
	dirs := make([]string, 0, len(list))
	for _, s := range list {
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
	return allowed(ws, trees, append(uploads, wikiDirs(ctx, ws)...))
}

func wikiDirs(ctx context.Context, ws contract.Workspace) []string {
	var dirs []string
	for _, f := range ws.Folders {
		p, ok, err := wikiroot.Find(ctx, f.Path)
		if err != nil {
			logError("wiki of "+f.Path, err)
			continue
		}
		if ok && !slices.Contains(dirs, p.Dir) {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
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
