package main

import (
	"context"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
)

type sessions struct {
	h     *pluginsdk.Host
	mu    sync.Mutex
	cache map[string]map[string]api.Session
	gen   map[string]int
	epoch int
}

func (s *sessions) lookup(ctx context.Context, workspaceID string) (map[string]api.Session, bool) {
	s.mu.Lock()
	m, ok := s.cache[workspaceID]
	s.mu.Unlock()
	if ok {
		return m, true
	}
	return s.fetch(ctx, workspaceID)
}

func (s *sessions) fetch(ctx context.Context, workspaceID string) (map[string]api.Session, bool) {
	s.mu.Lock()
	gen, epoch := s.gen[workspaceID], s.epoch
	s.mu.Unlock()

	list, err := agentcall.Sessions(ctx, s.h, workspaceID)
	if err != nil {
		return nil, false
	}
	m := make(map[string]api.Session, len(list))
	for _, a := range list {
		m[a.ID] = a
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen[workspaceID] == gen && s.epoch == epoch {
		if s.cache == nil {
			s.cache = map[string]map[string]api.Session{}
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

func (s *sessions) title(ctx context.Context, workspaceID, id string) (string, bool) {
	m, ok := s.lookup(ctx, workspaceID)
	if !ok {
		return "", false
	}
	a, found := m[id]
	return a.Title, found
}
