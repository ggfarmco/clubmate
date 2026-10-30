package main

import (
	"context"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
)

const agentPrefix = "agt_"

type agentSession struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	ParentSession string `json:"parentSession"`
	Title         string `json:"title"`
	Closed        bool   `json:"closed"`
}

type sessions struct {
	h     *pluginsdk.Host
	mu    sync.Mutex
	cache map[string]map[string]agentSession
	gen   map[string]int
	epoch int
}

func (s *sessions) lookup(ctx context.Context, projectID string) (map[string]agentSession, bool) {
	s.mu.Lock()
	m, ok := s.cache[projectID]
	s.mu.Unlock()
	if ok {
		return m, true
	}
	return s.fetch(ctx, projectID)
}

func (s *sessions) fetch(ctx context.Context, projectID string) (map[string]agentSession, bool) {
	s.mu.Lock()
	gen, epoch := s.gen[projectID], s.epoch
	s.mu.Unlock()

	var list []agentSession
	if err := s.h.RunCommand(ctx, "agents.sessions", projectID, map[string]any{}, &list); err != nil {
		return nil, false
	}
	m := make(map[string]agentSession, len(list))
	for _, a := range list {
		m[a.ID] = a
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen[projectID] == gen && s.epoch == epoch {
		if s.cache == nil {
			s.cache = map[string]map[string]agentSession{}
		}
		s.cache[projectID] = m
	}
	return m, true
}

func (s *sessions) invalidate(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == nil {
		s.gen = map[string]int{}
	}
	s.gen[projectID]++
	delete(s.cache, projectID)
}

func (s *sessions) invalidateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	s.cache = nil
}

func (s *sessions) drop(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, projectID)
	delete(s.gen, projectID)
}

func (s *sessions) stale(ctx context.Context, projectID, owner string) bool {
	if !strings.HasPrefix(owner, agentPrefix) {
		return false
	}
	m, ok := s.lookup(ctx, projectID)
	if !ok {
		return false
	}
	if _, found := m[owner]; !found {
		if m, ok = s.fetch(ctx, projectID); !ok {
			return false
		}
	}
	a, found := m[owner]
	return !found || a.Closed || a.Status == "closed"
}

func (s *sessions) ancestor(ctx context.Context, projectID, ancestor, owner string) bool {
	m, ok := s.lookup(ctx, projectID)
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

func (s *sessions) title(ctx context.Context, projectID, id string) (string, bool) {
	m, ok := s.lookup(ctx, projectID)
	if !ok {
		return "", false
	}
	a, found := m[id]
	return a.Title, found
}
