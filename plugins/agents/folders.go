package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

var (
	errNotFolder  = errors.New("not a folder of this workspace")
	errFolderUser = errors.New("only the user removes a folder")
)

type folderSession struct {
	Session  string `json:"session"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
}

type folderWorktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

type folderRemoveResult struct {
	Removed  bool            `json:"removed"`
	Blockers []folderSession `json:"blockers,omitempty"`
	Tasks    []string        `json:"tasks,omitempty"`
}

type folderImpactResult struct {
	Sessions  []folderSession  `json:"sessions"`
	Worktrees []folderWorktree `json:"worktrees"`
}

func (a *app) folderRemoveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errFolderUser
	}
	folder, err := a.workspaceFolder(ctx, cmd.WorkspaceID, args.Path)
	if err != nil {
		return nil, err
	}
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	var blockers []folderSession
	for _, s := range a.sessionsIn(ctx, cmd.WorkspaceID, folder) {
		if s.blocksFolder() {
			blockers = append(blockers, folderSessionOf(s.snapshot()))
		}
	}
	tasks := a.tasksOn(cmd.WorkspaceID, folder)
	if len(blockers) > 0 || len(tasks) > 0 {
		return folderRemoveResult{Blockers: blockers, Tasks: tasks}, nil
	}
	if _, err := a.host.RemoveFolder(ctx, cmd.WorkspaceID, folder); err != nil {
		return nil, err
	}
	return folderRemoveResult{Removed: true}, nil
}

func (a *app) folderImpactCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	folder, err := a.workspaceFolder(ctx, cmd.WorkspaceID, args.Path)
	if err != nil {
		return nil, err
	}
	out := folderImpactResult{Sessions: []folderSession{}, Worktrees: []folderWorktree{}}
	for _, s := range a.sessionsIn(ctx, cmd.WorkspaceID, folder) {
		out.Sessions = append(out.Sessions, folderSessionOf(s.snapshot()))
	}
	if f, err := gitrepo.OpenFolder(ctx, folder); err == nil {
		for _, t := range f.Trees[1:] {
			if !t.Bare {
				out.Worktrees = append(out.Worktrees, folderWorktree{Path: t.Path, Branch: t.Branch})
			}
		}
	}
	return out, nil
}

func (a *app) workspaceFolder(ctx context.Context, workspaceID, path string) (string, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	folder, ok := folderIn(w, path)
	if !ok {
		return "", fmt.Errorf("%w: %s", errNotFolder, path)
	}
	return folder, nil
}

func (a *app) sessionsIn(ctx context.Context, workspaceID, folder string) []*session {
	dirs := folderDirs(ctx, folder)
	return slices.DeleteFunc(a.workspaceSessions(workspaceID), func(s *session) bool {
		worktree := gitrepo.Canonical(s.snapshot().Worktree)
		return !slices.ContainsFunc(dirs, func(dir string) bool { return gitrepo.Within(worktree, dir) })
	})
}

func folderDirs(ctx context.Context, folder string) []string {
	f, err := gitrepo.OpenFolder(ctx, folder)
	if err != nil {
		return []string{gitrepo.Canonical(folder)}
	}
	dirs := make([]string, 0, len(f.Trees))
	for _, t := range f.Trees {
		dirs = append(dirs, f.Mapped(t))
	}
	return dirs
}

func folderSessionOf(rec record) folderSession {
	return folderSession{Session: rec.ID, Title: rec.Title, Status: rec.Status, Worktree: rec.Worktree}
}

func (s *session) blocksFolder() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.closed() {
		return false
	}
	return s.rec.Status == statusRunning || s.rec.Status == statusWaiting || len(s.queue) > 0 || s.setup.pending()
}
