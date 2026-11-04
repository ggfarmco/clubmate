package main

import (
	"context"
	"slices"
	"time"
)

const (
	closeAfter    = time.Hour
	closeAfterEnv = "CLUBMATE_AGENTS_CLOSE_AFTER"
)

type ownedJob struct {
	Owner string `json:"owner"`
}

type listedRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (a *app) closeDoneLoop(after time.Duration) {
	if after <= 0 {
		return
	}
	t := time.NewTicker(min(suspendTick, after/2))
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.closeDone(after)
		}
	}
}

func (a *app) closeDone(after time.Duration) {
	a.mu.Lock()
	var roots []*session
	for id, s := range a.sessions {
		if rec := s.snapshot(); a.dropped[id] == 0 && rec.ParentSession == "" && !rec.restricted() && !rec.closed() {
			roots = append(roots, s)
		}
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	owners := map[string]map[string]bool{}
	stopped := map[string]map[string]bool{}
	for _, root := range roots {
		tree := a.subtree(root)
		if slices.ContainsFunc(tree, func(s *session) bool { return !s.settled(after) }) {
			continue
		}
		rec := root.snapshot()
		workspace := rec.Workspace
		if rec.Run != nil {
			if _, ok := stopped[workspace]; !ok {
				stopped[workspace] = a.stoppedRuns(ctx, workspace)
			}
			if !stopped[workspace][rec.Run.ID] {
				continue
			}
		}
		if _, ok := owners[workspace]; !ok {
			owners[workspace] = a.jobOwners(ctx, workspace)
		}
		if owned := owners[workspace]; owned == nil || slices.ContainsFunc(tree, func(s *session) bool { return owned[s.snapshot().ID] }) {
			continue
		}
		for _, s := range tree {
			if !s.snapshot().closed() {
				a.closeIdle(ctx, s)
			}
		}
	}
}

func (a *app) jobOwners(ctx context.Context, workspace string) map[string]bool {
	var cron struct {
		Jobs []ownedJob `json:"jobs"`
	}
	var triggers struct {
		Triggers []ownedJob `json:"triggers"`
	}
	if a.host.RunCommand(ctx, "cron.list", workspace, struct{}{}, &cron) != nil || a.host.RunCommand(ctx, "triggers.list", workspace, struct{}{}, &triggers) != nil {
		return nil
	}
	owned := map[string]bool{}
	for _, j := range append(cron.Jobs, triggers.Triggers...) {
		owned[j.Owner] = true
	}
	return owned
}

func (a *app) stoppedRuns(ctx context.Context, workspace string) map[string]bool {
	var list struct {
		Runs []listedRun `json:"runs"`
	}
	if a.host.RunCommand(ctx, "graphs.list", workspace, struct{}{}, &list) != nil {
		return nil
	}
	stopped := map[string]bool{}
	for _, r := range list.Runs {
		if r.Status == "stopped" {
			stopped[r.ID] = true
		}
	}
	return stopped
}

func (s *session) settled(after time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec.closed() || s.rec.Status == statusDone && !s.rec.Review && s.idleLocked(after) && !s.asksLocked()
}
