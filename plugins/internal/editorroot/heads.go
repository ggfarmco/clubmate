package editorroot

import (
	"context"
	"path/filepath"
	"slices"
	"sync"

	"clubmate/pkg/pluginsdk"
)

type Heads struct {
	mu      sync.Mutex
	watches map[string]headWatch
	deleted func(string) bool
}

type headWatch struct {
	id      string
	folders []string
}

func NewHeads(deleted func(string) bool) *Heads {
	return &Heads{watches: map[string]headWatch{}, deleted: deleted}
}

func (w *Heads) Watch(ctx context.Context, h *pluginsdk.Host, p pluginsdk.Workspace) error {
	folders := slices.Concat(p.Paths(), p.Roots())
	w.mu.Lock()
	defer w.mu.Unlock()
	old, ok := w.watches[p.ID]
	if w.deleted(p.ID) || ok && slices.Equal(old.folders, folders) {
		return nil
	}
	var err error
	if ok {
		delete(w.watches, p.ID)
		err = h.UnwatchFS(ctx, old.id)
	}
	id, werr := h.WatchFS(ctx, p.ID)
	if werr != nil {
		return werr
	}
	w.watches[p.ID] = headWatch{id: id, folders: folders}
	return err
}

func (w *Heads) Unwatch(ctx context.Context, h *pluginsdk.Host, workspaceID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	old, ok := w.watches[workspaceID]
	if !ok {
		return nil
	}
	delete(w.watches, workspaceID)
	return h.UnwatchFS(ctx, old.id)
}

func (w *Heads) Moved(ev pluginsdk.FSChange) bool {
	w.mu.Lock()
	cur, ok := w.watches[ev.WorkspaceID]
	w.mu.Unlock()
	return ok && cur.id == ev.Watch && (ev.Overflow || slices.ContainsFunc(ev.Paths, isHead))
}

func isHead(path string) bool {
	return filepath.Base(path) == "HEAD"
}
