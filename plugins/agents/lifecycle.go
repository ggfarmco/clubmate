package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
)

const (
	stopRootWait = 4 * time.Second
)

type dropState int

const (
	dropClosed dropState = iota + 1
	dropRemoved
)

func (a *app) closeTree(ctx context.Context, s *session) {
	seen := map[string]bool{}
	var walk func(*session)
	walk = func(x *session) {
		id := x.snapshot().ID
		if seen[id] {
			return
		}
		seen[id] = true
		a.closeSession(ctx, x, false)
		a.spawnMu.Lock()
		children := a.children(id)
		a.spawnMu.Unlock()
		for _, c := range children {
			walk(c)
		}
	}
	walk(s)
}

func (a *app) deleteTree(ctx context.Context, s *session) error {
	seen := map[string]bool{}
	var walk func(*session) error
	walk = func(x *session) error {
		id := x.snapshot().ID
		if seen[id] {
			return nil
		}
		seen[id] = true
		a.closeSession(ctx, x, true)
		a.spawnMu.Lock()
		children := a.children(id)
		a.spawnMu.Unlock()
		var errs []error
		for _, c := range children {
			errs = append(errs, walk(c))
		}
		return errors.Join(append(errs, a.deleteSession(ctx, x, true))...)
	}
	return walk(s)
}

func (a *app) closeSession(ctx context.Context, s *session, remove bool) *agentProc {
	s.mu.Lock()
	p, agentID, running, rec := s.proc, s.rec.AgentSessionID, s.running, s.rec
	s.proc = nil
	s.running = false
	s.queue = nil
	s.cancelPermissionsLocked()
	if !s.rec.Closed {
		s.recordLocked(actorUser, kindClose, acp.SessionRequest{SessionID: agentID})
		s.rec.Closed = true
		s.setStatusLocked(statusClosed, "")
	}
	ended := s.turnLocked()
	s.mu.Unlock()
	if running {
		s.turned(ended)
	}
	s.changed(changeStatus, true)
	a.cancelSignIn(ctx, rec.Workspace, rec.ID)
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req := acp.SessionRequest{SessionID: agentID}
	if running {
		_ = p.conn.Notify(acp.MethodSessionCancel, req)
	}
	if p.caps.SessionCapabilities.Close != nil {
		_ = p.conn.Call(ctx, acp.MethodSessionClose, req, nil)
	}
	if remove && p.caps.SessionCapabilities.Delete != nil {
		_ = p.conn.Call(ctx, acp.MethodSessionDelete, req, nil)
	}
	p.unregister(agentID)
	a.release(p)
	return p
}

func (a *app) deleteSession(ctx context.Context, s *session, remote bool) error {
	a.closeSession(ctx, s, remote)
	a.mu.Lock()
	delete(a.sessions, s.rec.ID)
	delete(a.dropped, s.rec.ID)
	a.mu.Unlock()
	s.mu.Lock()
	s.deleted = true
	rec := s.rec
	err := s.log.remove()
	s.mu.Unlock()
	a.notes.mark(s, changeDeleted, true)
	a.forgetSession(ctx, rec.Workspace, rec.ID)
	a.kick(rec.Workspace)
	refsCtx, cancel := context.WithTimeout(context.Background(), refsTimeout)
	defer cancel()
	return errors.Join(err, a.deleteRefs(refsCtx, rec))
}

func (a *app) deleteRefs(ctx context.Context, rec record) error {
	if _, err := os.Stat(rec.Worktree); err == nil {
		return deleteCheckpoints(ctx, rec.Worktree, rec.ID)
	}
	p, err := a.host.GetProject(ctx, rec.Workspace)
	if err != nil {
		return nil
	}
	var errs []error
	for _, repo := range p.Repos {
		errs = append(errs, deleteCheckpoints(ctx, repo, rec.ID))
	}
	return errors.Join(errs...)
}

func (a *app) stopRoot(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, stopRootWait)
	defer cancel()
	root = gitrepo.Canonical(root)
	inside := func(dir string) bool { return gitrepo.Within(gitrepo.Canonical(dir), root) }
	a.mu.Lock()
	var hit []*session
	for id, s := range a.sessions {
		if inside(s.snapshot().Worktree) {
			a.dropped[id] = max(a.dropped[id], dropClosed)
			hit = append(hit, s)
		}
	}
	a.mu.Unlock()
	for _, s := range hit {
		a.closeSession(ctx, s, false)
	}
	a.mu.Lock()
	var procs []*agentProc
	for p := range a.live {
		if inside(p.root) {
			procs = append(procs, p)
		}
	}
	for k, slot := range a.procs {
		if slot.proc != nil && inside(slot.proc.root) {
			delete(a.procs, k)
		}
	}
	a.mu.Unlock()
	for _, p := range procs {
		go p.shutdown()
	}
	for _, p := range procs {
		select {
		case <-p.exited:
		case <-ctx.Done():
		}
	}
	refsCtx, refsCancel := context.WithTimeout(context.WithoutCancel(ctx), refsTimeout)
	defer refsCancel()
	var errs []error
	for _, s := range hit {
		rec := s.snapshot()
		errs = append(errs, deleteCheckpoints(refsCtx, rec.Worktree, rec.ID))
	}
	return errors.Join(errs...)
}

func (a *app) dropWorkspace(projectID string, match func(record) bool, remove bool) {
	ctx, cancel := context.WithTimeout(context.Background(), refsTimeout)
	defer cancel()
	want := dropClosed
	if remove {
		want = dropRemoved
	}
	a.mu.Lock()
	all := slices.Collect(maps.Values(a.sessions))
	a.mu.Unlock()
	for _, s := range all {
		rec := s.snapshot()
		if rec.Workspace != projectID || (match != nil && !match(rec)) || !a.claimDrop(rec.ID, want) {
			continue
		}
		var err error
		if remove {
			err = a.deleteSession(ctx, s, false)
		} else {
			a.closeSession(ctx, s, false)
			err = a.deleteRefs(ctx, rec)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: drop session:", err)
		}
	}
}

func (a *app) claimDrop(id string, want dropState) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[id] == nil || a.dropped[id] >= want {
		return false
	}
	a.dropped[id] = want
	return true
}
