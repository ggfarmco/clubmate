package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"
)

const (
	deleteAfter    = 30 * 24 * time.Hour
	deleteAfterEnv = "CLUBMATE_AGENTS_DELETE_AFTER"
	deleteTick     = time.Hour
)

func (a *app) deleteOldLoop(after time.Duration) {
	if after <= 0 {
		return
	}
	a.deleteOld(after)
	t := time.NewTicker(min(deleteTick, after/2))
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.deleteOld(after)
		}
	}
}

func (a *app) deleteOld(after time.Duration) {
	cutoff := time.Now().Add(-after)
	a.mu.Lock()
	var roots []*session
	for id, s := range a.sessions {
		rec := s.snapshot()
		if a.dropped[id] != 0 || rec.Night != "" {
			continue
		}
		if _, parent := a.sessions[rec.ParentSession]; !parent {
			roots = append(roots, s)
		}
	}
	a.mu.Unlock()
	for _, root := range roots {
		if slices.ContainsFunc(a.subtree(root), func(s *session) bool { return !s.snapshot().expired(cutoff) }) {
			continue
		}
		if err := a.deleteTree(context.Background(), root); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete old session:", err)
		}
	}
}

func (r record) expired(cutoff time.Time) bool {
	return r.closed() && r.Night == "" && !r.taskOpen() && r.UpdatedAt.Before(cutoff)
}
