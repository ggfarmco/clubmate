package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	openErrorEvent = "open_error"
	reviewEvent    = "review"
	docOpenEvent   = "docOpen"
	openErrorMax   = 80
	fileTopic      = "editor-nvim.file"
)

type fileEvent struct {
	Workspace string `json:"workspace"`
	Root      string `json:"root"`
	Path      string `json:"path"`
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
	if ev.Event == reviewEvent {
		a.reviewEvent(h, e, ev)
		return
	}
	if ev.Event == docOpenEvent {
		go a.openDoc(h, e, ev.File)
		return
	}
	branch, refresh := "", ev.Event == "BufEnter" || ev.Event == "DirChanged"
	if refresh {
		branch = gitrepo.BranchAt(ctx, branchDir(ev.File, e.root))
	}
	e.update(ev, branch, refresh)
	a.scheduleStatus(h, e)
	if ev.Event == "BufEnter" && e.share(ev.File) {
		if _, err := h.Publish(ctx, fileTopic, fileEvent{Workspace: e.id, Root: e.root, Path: ev.File}); err != nil {
			logError("publish file", err)
		}
	}
}

func (a *app) refreshStatus(h *pluginsdk.Host, workspaceID string) {
	for _, e := range a.workspaceEditors(workspaceID) {
		if a.isCurrent(e) {
			a.scheduleStatus(h, e)
		}
	}
}

func (a *app) openDoc(h *pluginsdk.Host, e *editor, path string) {
	if err := h.RunCommand(context.Background(), "docs.open", e.id, map[string]string{"path": path}, nil); err != nil {
		e.setOpenError(shortError(err.Error()))
		a.scheduleStatus(h, e)
	}
}

func (a *app) fsChanged(_ context.Context, h *pluginsdk.Host, ev pluginsdk.FSChange) {
	if a.heads.Moved(ev) {
		go a.refreshBranches(h, ev.WorkspaceID)
	}
	if ev.Overflow || slices.ContainsFunc(ev.Paths, isReviewDB) {
		go a.reviewDBChanged(context.Background(), h, ev.WorkspaceID)
	}
}

func (a *app) watch(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Workspace) {
	if err := a.heads.Watch(ctx, h, p); err != nil {
		logError("watch "+p.ID, err)
	}
}

func (a *app) unwatch(ctx context.Context, h *pluginsdk.Host, workspaceID string) {
	if err := a.heads.Unwatch(ctx, h, workspaceID); err != nil {
		logError("unwatch "+workspaceID, err)
	}
}

func (a *app) refreshBranches(h *pluginsdk.Host, workspaceID string) {
	a.branchMu.Lock()
	defer a.branchMu.Unlock()
	for _, e := range a.workspaceEditors(workspaceID) {
		if e.connected() == nil {
			continue
		}
		file := e.current().File
		if e.setBranch(file, gitrepo.BranchAt(context.Background(), branchDir(file, e.root))) {
			a.scheduleStatus(h, e)
		}
	}
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
