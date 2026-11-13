package main

import (
	"context"
	"fmt"
	"os"
	"slices"

	"clubmate/plugins/internal/gitrepo"
)

const maxReviewBases = 12

type changeState struct {
	changes      bool
	changesFresh bool
	reviewBases  []string
}

func (a *app) refreshChanges(s *session) {
	s.changesMu.Lock()
	defer s.changesMu.Unlock()
	workspace, root, base := s.changesBase()
	changes := false
	var bases []string
	if base != "" {
		ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
		defer cancel()
		var err error
		if changes, err = changedSince(ctx, root, base); err != nil {
			fmt.Fprintln(os.Stderr, "agents: changes:", err)
		}
		if changes {
			bases = reviewBases(ctx, root)
		}
	}
	s.mu.Lock()
	differs := s.changes != changes || !slices.Equal(s.reviewBases, bases)
	s.changes, s.reviewBases = changes, bases
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

func (s *session) hasChanges() (bool, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes, s.reviewBases
}

func reviewBases(ctx context.Context, root string) []string {
	local, _, err := gitrepo.RecentBranches(ctx, root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: review bases:", err)
		return nil
	}
	own := gitrepo.BranchAt(ctx, root)
	var bases []string
	if def, err := gitrepo.DefaultBranch(ctx, root); err == nil && def != own && slices.Contains(local, def) {
		bases = append(bases, def)
	}
	for _, b := range local {
		if len(bases) == maxReviewBases {
			break
		}
		if b != own && !slices.Contains(bases, b) {
			bases = append(bases, b)
		}
	}
	return bases
}

func (s *session) changesBase() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.rec.Workspace, s.rec.Worktree, firstCheckpoint(s.turns).CheckpointSha
}
