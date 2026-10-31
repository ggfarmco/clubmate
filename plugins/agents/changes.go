package main

import (
	"context"
	"fmt"
	"os"
)

type changeState struct {
	changes      bool
	changesFresh bool
}

func (a *app) refreshChanges(s *session) {
	s.changesMu.Lock()
	defer s.changesMu.Unlock()
	workspace, root, base := s.changesBase()
	changes := false
	if base != "" {
		ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
		defer cancel()
		var err error
		if changes, err = changedSince(ctx, root, base); err != nil {
			fmt.Fprintln(os.Stderr, "agents: changes:", err)
		}
	}
	s.mu.Lock()
	differs := s.changes != changes
	s.changes = changes
	s.mu.Unlock()
	if differs {
		a.kick(workspace)
	}
}

func (s *session) claimChangesCheck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if s.changesFresh {
		return false
	}
	s.changesFresh = true
	return true
}

func (s *session) invalidateChanges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changesFresh = false
}

func (s *session) hasChanges() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

func (s *session) changesBase() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.rec.Workspace, s.rec.Worktree, firstCheckpoint(s.turns).CheckpointSha
}
