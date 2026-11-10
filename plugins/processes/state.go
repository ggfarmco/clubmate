package main

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

var errWorkspaceDeleted = errors.New("workspace deleted")

type workspaceState struct {
	session   string
	pid       int
	filter    string
	coreOpen  bool
	auto      bool
	options   bool
	sort      sdui.Sort
	collapsed map[int]bool
	folded    map[string]bool
	pidDepth  int
	sidebar   *sidebarInputs
}

type sidebarInputs struct {
	sessions []group
	cpu      map[string]string
	total    int
	selected string
}

func (a *app) state(ctx context.Context, h *pluginsdk.Host, id string) (*workspaceState, error) {
	a.mu.Lock()
	st, ok := a.workspaces[id]
	_, gone := a.deleted[id]
	a.mu.Unlock()
	if gone {
		return nil, errWorkspaceDeleted
	}
	if ok {
		return st, nil
	}
	loaded := &workspaceState{collapsed: map[int]bool{}, folded: map[string]bool{}}
	if _, err := h.StorageGet(ctx, selectedKey(id), &loaded.session); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, autoKillKey(id), &loaded.auto); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, optionsKey(id), &loaded.options); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, sortKey(id), &loaded.sort); err != nil {
		return nil, err
	}
	if !validSort(loaded.sort) {
		loaded.sort = defaultSort
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, gone := a.deleted[id]; gone {
		return nil, errWorkspaceDeleted
	}
	if st, ok := a.workspaces[id]; ok {
		return st, nil
	}
	a.workspaces[id] = loaded
	return loaded, nil
}

func (a *app) snapshot(st *workspaceState) workspaceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := *st
	c.collapsed = maps.Clone(st.collapsed)
	c.folded = maps.Clone(st.folded)
	return c
}

func (a *app) setPresent(id string, on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if on {
		a.present[id] = true
	} else {
		delete(a.present, id)
	}
}

func (a *app) presentIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.present))
	for id := range a.present {
		ids = append(ids, id)
	}
	return ids
}

func (a *app) loadSection(ctx context.Context, h *pluginsdk.Host, id string) {
	section, err := h.SelectedSection(ctx, id)
	if err != nil {
		logError("section "+id, err)
		return
	}
	a.setSection(id, section)
}

func (a *app) setSection(id, section string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, gone := a.deleted[id]; !gone {
		a.sections[id] = section
	}
}

func (a *app) setWatched(id string, sections []string) bool {
	on := slices.Contains(sections, sectionID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, gone := a.deleted[id]; gone {
		return false
	}
	was := a.watched[id]
	if on {
		a.watched[id] = true
	} else {
		delete(a.watched, id)
	}
	return on && !was
}

func (a *app) visible(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	section := a.sections[id]
	return section == "" || section == sectionID || a.watched[id]
}

func (a *app) drop(ctx context.Context, h *pluginsdk.Host, id string) {
	a.forget(id, time.Now())
	logError("drop "+id, errors.Join(h.StorageDelete(ctx, selectedKey(id)), h.StorageDelete(ctx, autoKillKey(id)), h.StorageDelete(ctx, optionsKey(id)), h.StorageDelete(ctx, sortKey(id))))
}

func (a *app) forget(id string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.workspaces, id)
	delete(a.present, id)
	delete(a.sections, id)
	delete(a.watched, id)
	a.deleted[id] = now
}

func (a *app) pruneDeleted(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, at := range a.deleted {
		if now.Sub(at) >= deletedTTL {
			delete(a.deleted, id)
		}
	}
}

func storeOrDelete(ctx context.Context, h *pluginsdk.Host, key string, value any, keep bool) error {
	if !keep {
		return h.StorageDelete(ctx, key)
	}
	return h.StorageSet(ctx, key, value)
}

func selectedKey(workspaceID string) string {
	return "selected/" + workspaceID
}

func autoKillKey(workspaceID string) string {
	return autoKillPrefix + workspaceID
}

func sortKey(workspaceID string) string {
	return "sort/" + workspaceID
}

func optionsKey(workspaceID string) string {
	return "options/" + workspaceID
}
