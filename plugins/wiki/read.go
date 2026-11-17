package main

import (
	"context"
	"maps"
	"time"
)

const (
	newCount      = 5
	readKeyPrefix = "read/"
)

type readState struct {
	Since  time.Time            `json:"since"`
	Opened map[string]time.Time `json:"opened"`
}

func readKey(workspaceID string) string {
	return readKeyPrefix + workspaceID
}

func (w *wiki) readState(ctx context.Context, workspaceID string, u *workspaceUI) readState {
	w.mu.Lock()
	cached := u.read
	w.mu.Unlock()
	if cached != nil {
		return *cached
	}
	var st readState
	ok, err := w.h.StorageGet(ctx, readKey(workspaceID), &st)
	if err != nil {
		logError("read state "+workspaceID, err)
		return readState{Since: w.now().UTC(), Opened: map[string]time.Time{}}
	}
	if !ok || st.Since.IsZero() {
		st.Since = w.now().UTC()
		logError("save read state "+workspaceID, w.h.StorageSet(ctx, readKey(workspaceID), st))
	}
	if st.Opened == nil {
		st.Opened = map[string]time.Time{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if u.read == nil {
		u.read = &st
	}
	return *u.read
}

func (w *wiki) markOpened(ctx context.Context, workspaceID string, u *workspaceUI, row recentRow, all []recentRow) {
	id := pageID(row.Project, row.Page.Path)
	w.mu.Lock()
	if u.read == nil || !row.Page.Updated.After(u.read.Opened[id]) {
		w.mu.Unlock()
		return
	}
	opened := map[string]time.Time{id: row.Page.Updated}
	for _, r := range all {
		k := pageID(r.Project, r.Page.Path)
		if t, ok := u.read.Opened[k]; ok && k != id {
			opened[k] = t
		}
	}
	u.read.Opened = opened
	st := readState{Since: u.read.Since, Opened: maps.Clone(opened)}
	w.mu.Unlock()
	logError("save read state "+workspaceID, w.h.StorageSet(ctx, readKey(workspaceID), st))
}

func newToYou(all []recentRow, st readState) []recentRow {
	var out []recentRow
	for _, r := range all {
		if len(out) == newCount {
			break
		}
		if isNew(r, st) {
			out = append(out, r)
		}
	}
	return out
}

func isNew(r recentRow, st readState) bool {
	if r.Page.Shadow || r.Page.By == userName || r.Page.Updated.IsZero() {
		return false
	}
	seen, ok := st.Opened[pageID(r.Project, r.Page.Path)]
	if !ok {
		seen = st.Since
	}
	return r.Page.Updated.After(seen)
}
