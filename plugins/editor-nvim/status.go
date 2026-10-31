package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	branchTimeout  = 2 * time.Second
	openErrorEvent = "open_error"
	openErrorMax   = 80
)

type projectWatch struct {
	id      string
	folders []string
}

func (a *app) nvimEvent(h *pluginsdk.Host, e *editor, ev nvimEvent) {
	ctx := context.Background()
	if ev.Event == openErrorEvent {
		e.setOpenError(shortError(ev.Message))
		a.scheduleStatus(h, e)
		return
	}
	if ev.Event == importEvent {
		go a.importFromEditor(h, e)
		return
	}
	branch, refresh := "", ev.Event == "BufEnter" || ev.Event == "DirChanged"
	if refresh {
		branch = gitBranch(ctx, branchDir(ev.File, e.root))
	}
	e.update(ev, branch, refresh)
	a.scheduleStatus(h, e)
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	a.watchMu.Lock()
	w, ok := a.watches[ev.ProjectID]
	a.watchMu.Unlock()
	if ok && w.id == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, isHead)) {
		go a.refreshBranches(h, ev.ProjectID)
	}
}

func (a *app) watch(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Project) {
	folders := slices.Concat(p.Repos, p.Roots)
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	old, ok := a.watches[p.ID]
	if a.isDeleted(p.ID) || ok && slices.Equal(old.folders, folders) {
		return
	}
	if ok {
		delete(a.watches, p.ID)
		if err := h.UnwatchFS(ctx, old.id); err != nil {
			logError("unwatch "+p.ID, err)
		}
	}
	id, err := h.WatchFS(ctx, p.ID)
	if err != nil {
		logError("watch "+p.ID, err)
		return
	}
	a.watches[p.ID] = projectWatch{id: id, folders: folders}
}

func (a *app) unwatch(ctx context.Context, h *pluginsdk.Host, projectID string) {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if w, ok := a.watches[projectID]; ok {
		delete(a.watches, projectID)
		if err := h.UnwatchFS(ctx, w.id); err != nil {
			logError("unwatch "+projectID, err)
		}
	}
}

func (a *app) refreshBranches(h *pluginsdk.Host, projectID string) {
	a.branchMu.Lock()
	defer a.branchMu.Unlock()
	for _, e := range a.projectEditors(projectID) {
		if e.connected() == nil {
			continue
		}
		file := e.current().File
		if e.setBranch(file, gitBranch(context.Background(), branchDir(file, e.root))) {
			a.scheduleStatus(h, e)
		}
	}
}

func isHead(path string) bool {
	return filepath.Base(path) == "HEAD"
}

func shortError(msg string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	runes := []rune(first)
	if len(runes) > openErrorMax {
		return string(runes[:openErrorMax]) + "..."
	}
	return first
}

func branchDir(file, root string) string {
	if file != "" {
		return filepath.Dir(file)
	}
	return root
}

func gitBranch(ctx context.Context, dir string) string {
	repo, ok := gitrepo.RepoRoot(dir)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, branchTimeout)
	defer cancel()
	branch, err := gitrepo.Branch(ctx, repo)
	if err != nil {
		return ""
	}
	return branch
}
