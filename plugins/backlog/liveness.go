package main

import (
	"context"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

type agentSession struct {
	api.Session
	Run      *api.Run      `json:"run,omitempty"`
	NeedsYou bool          `json:"needsYou,omitempty"`
	Turns    int           `json:"turns,omitempty"`
	Failure  *agentFailure `json:"failure,omitempty"`
}

type agentFailure struct {
	Badge string `json:"badge"`
}

func (s agentSession) failure() string {
	if s.Status != sessionError {
		return ""
	}
	if s.Failure != nil {
		if _, detail, ok := strings.Cut(s.Failure.Badge, " · "); ok {
			return detail
		}
	}
	return sessionError
}

type sessions struct {
	h     *pluginsdk.Host
	mu    sync.Mutex
	cache map[string]map[string]agentSession
	gen   map[string]int
	epoch int
}

func (s *sessions) lookup(ctx context.Context, workspaceID string) (map[string]agentSession, bool) {
	s.mu.Lock()
	m, ok := s.cache[workspaceID]
	s.mu.Unlock()
	if ok {
		return m, true
	}
	return s.fetch(ctx, workspaceID)
}

func (s *sessions) fetch(ctx context.Context, workspaceID string) (map[string]agentSession, bool) {
	s.mu.Lock()
	gen, epoch := s.gen[workspaceID], s.epoch
	s.mu.Unlock()

	var list []agentSession
	if err := s.h.RunCommand(ctx, "agents.sessions", workspaceID, struct{}{}, &list); err != nil {
		return nil, false
	}
	m := make(map[string]agentSession, len(list))
	for _, a := range list {
		m[a.ID] = a
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen[workspaceID] == gen && s.epoch == epoch {
		if s.cache == nil {
			s.cache = map[string]map[string]agentSession{}
		}
		s.cache[workspaceID] = m
	}
	return m, true
}

func (s *sessions) invalidate(workspaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == nil {
		s.gen = map[string]int{}
	}
	s.gen[workspaceID]++
	delete(s.cache, workspaceID)
}

func (s *sessions) invalidateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	s.cache = nil
}

func (s *sessions) drop(workspaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, workspaceID)
	delete(s.gen, workspaceID)
}

func (s *sessions) stale(ctx context.Context, workspaceID, owner string) bool {
	if !strings.HasPrefix(owner, sessionPfx) {
		return false
	}
	m, ok := s.lookup(ctx, workspaceID)
	if !ok {
		return false
	}
	if _, found := m[owner]; !found {
		if m, ok = s.fetch(ctx, workspaceID); !ok {
			return false
		}
	}
	a, found := m[owner]
	return !found || a.Closed()
}

func (s *sessions) ancestor(ctx context.Context, workspaceID, ancestor, owner string) bool {
	m, ok := s.lookup(ctx, workspaceID)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for id := m[owner].ParentSession; id != "" && !seen[id]; id = m[id].ParentSession {
		if id == ancestor {
			return true
		}
		seen[id] = true
	}
	return false
}

func (s *sessions) session(ctx context.Context, workspaceID, id string) (agentSession, bool) {
	m, ok := s.lookup(ctx, workspaceID)
	if !ok {
		return agentSession{}, false
	}
	a, found := m[id]
	return a, found
}

func (s *sessions) maybeSession(ctx context.Context, workspaceID, id string) bool {
	m, ok := s.lookup(ctx, workspaceID)
	if !ok {
		return true
	}
	_, found := m[id]
	return found
}
