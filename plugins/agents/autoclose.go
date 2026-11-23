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

func (a *app) closeDoneLoop(after time.Duration) {
	if after <= 0 {
		return
	}
	t := time.NewTicker(max(min(suspendTick, after/2), time.Millisecond))
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
		if rec := s.snapshot(); a.dropped[id] == 0 && a.orphanLocked(rec) && !rec.restricted() && !rec.closed() {
			roots = append(roots, s)
		}
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	owners := map[string]map[string]bool{}
	running := map[string]map[string]bool{}
	for _, root := range roots {
		rec := root.snapshot()
		tree := a.subtree(root)
		if slices.ContainsFunc(tree, func(s *session) bool {
			return !s.settled(after, s == root && rec.Run != nil) || a.gateBusy(ctx, s.snapshot())
		}) || !a.asidesSettled(tree, after) {
			continue
		}
		workspace := rec.Workspace
		if rec.Run != nil {
			if _, ok := running[workspace]; !ok {
				running[workspace] = a.runningRuns(workspace)
			}
			if live := running[workspace]; live == nil || live[rec.Run.ID] {
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
				a.closeUnlessBusy(ctx, s, s != root || rec.ParentSession != "", s == root && rec.Run != nil)
			}
		}
	}
}

func (a *app) gateBusy(ctx context.Context, rec record) bool {
	if rec.reviewRun(a.gateRunOpen) != "" {
		return true
	}
	gf := rec.GateFor
	return gf != nil && gf.Run != "" && !rec.closed() && a.graphs.GateRunning(ctx, gf.Run)
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

func (a *app) orphanLocked(rec record) bool {
	if rec.ParentSession == "" {
		return true
	}
	parent, ok := a.sessions[rec.ParentSession]
	return !ok || parent.snapshot().closed()
}

func (a *app) runningRuns(workspace string) map[string]bool {
	return a.graphs.Running(workspace)
}

func (s *session) settled(after time.Duration, idleOK bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec.closed() || (s.rec.Status == statusDone || idleOK && s.rec.Status == statusIdle) && !s.rec.Review && s.rec.OpenCount == 0 && !s.rec.connectWaits() && !s.rec.gateWaits() && !s.rec.taskOpen() && s.idleLocked(after) && (s.rec.ParentSession != "" || !s.asksLocked())
}

func (a *app) asidesSettled(tree []*session, after time.Duration) bool {
	for _, s := range tree {
		if slices.ContainsFunc(a.asides(s.snapshot().ID), func(x *session) bool { return !x.settled(after, true) }) {
			return false
		}
	}
	return true
}
