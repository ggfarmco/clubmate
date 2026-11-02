package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	statusReview  = "review"
	reviewedNode  = "reviewed"
	reviewedLabel = "Reviewed"
)

func (a *app) markReview(s *session) {
	rec := s.snapshot()
	if rec.ParentSession != "" || rec.Run != nil || rec.restricted() || rec.Finish == finishMerge {
		return
	}
	_, root, base := s.changesBase()
	if base == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	changed, err := changedSince(ctx, root, base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: review:", err)
		return
	}
	if !changed {
		return
	}
	s.mu.Lock()
	ok := s.rec.Status == statusDone && !s.running
	s.rec.Review = s.rec.Review || ok
	s.mu.Unlock()
	if ok {
		s.changed(changeUpdate, true)
	}
}

func (a *app) reviewedCommand(_ context.Context, _ pluginsdk.CommandRun, s *session) (any, error) {
	a.setReviewed(s)
	return nil, nil
}

func (a *app) setReviewed(s *session) {
	s.mu.Lock()
	was := s.rec.Review
	if was {
		s.rec.Review = false
		s.rec.UpdatedAt = time.Now().UTC()
	}
	s.mu.Unlock()
	if was {
		s.changed(changeUpdate, true)
	}
}
