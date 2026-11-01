package main

import (
	"maps"
	"slices"
	"time"
)

func (a *app) unloadIdle(after time.Duration) {
	shown := a.shownSessions()
	a.mu.Lock()
	sessions := slices.Collect(maps.Values(a.sessions))
	a.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		if !shown[s.rec.ID] && s.unloadableLocked(after) {
			s.unloadLocked()
		}
		s.mu.Unlock()
	}
}

func (a *app) shownSessions() map[string]bool {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	shown := map[string]bool{}
	for workspaceID := range a.present {
		if p := a.ui[workspaceID]; p != nil && p.selected != "" {
			shown[p.selected] = true
		}
	}
	return shown
}

func (s *session) unloadableLocked(after time.Duration) bool {
	return s.loaded && !s.deleted && s.proc == nil && !s.running && len(s.perms) == 0 && len(s.queue) == 0 && !s.setup.pending() && time.Since(s.used) >= after
}

func (s *session) unloadLocked() {
	s.log.flush()
	s.transcriptState = transcriptState{index: map[string]int{}, rev: s.rev}
	s.loaded = false
}
