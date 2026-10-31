package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/internal/proc"
	"clubmate/plugins/agents/acp"
)

const (
	suspendAfter    = 10 * time.Minute
	suspendTick     = time.Minute
	suspendAfterEnv = "CLUBMATE_AGENTS_SUSPEND_AFTER"
	startupGrace    = 30 * time.Second
)

var shells = []string{"sh", "bash", "zsh", "fish", "dash", "ksh", "tcsh", "csh"}

func (a *app) suspendIdleLoop(after time.Duration) {
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
			a.suspendIdle(after)
		}
	}
}

func (a *app) suspendIdle(after time.Duration) {
	a.mu.Lock()
	var idle []*session
	for id, s := range a.sessions {
		if a.dropped[id] == 0 {
			idle = append(idle, s)
		}
	}
	a.mu.Unlock()
	table := sync.OnceValues(proc.List)
	for _, s := range idle {
		if id := s.snapshot().ID; a.runningChildren(id) == 0 && a.resumes(id) {
			a.suspend(s, after, table)
		}
	}
}

func (a *app) suspend(s *session, after time.Duration, table func() ([]proc.Process, error)) {
	if !s.attachMu.TryLock() {
		return
	}
	defer s.attachMu.Unlock()
	s.mu.Lock()
	p, idle, prompted := s.proc, s.idleLocked(after), s.prompted
	s.mu.Unlock()
	if p == nil || !idle || prompted && p.caps.SessionCapabilities.Resume == nil {
		return
	}
	if procs, err := table(); err != nil || backgroundWork(p.pgid, procs) {
		return
	}
	s.mu.Lock()
	if s.proc != p || !s.idleLocked(after) {
		s.mu.Unlock()
		return
	}
	agentID := s.rec.AgentSessionID
	s.proc = nil
	s.rec.Suspended = true
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	if p.caps.SessionCapabilities.Close != nil {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		_ = p.conn.Call(ctx, acp.MethodSessionClose, acp.SessionRequest{SessionID: agentID}, nil)
		cancel()
	}
	p.unregister(agentID)
	a.release(p)
}

func (a *app) liveProc(s *session) (*agentProc, string, error) {
	s.mu.Lock()
	p, agentID, suspended := s.proc, s.rec.AgentSessionID, s.rec.Suspended
	s.mu.Unlock()
	if p != nil {
		return p, agentID, nil
	}
	if !suspended {
		return nil, "", errNotLive
	}
	p, err := a.attach(s)
	if err != nil {
		return nil, "", err
	}
	return p, s.snapshot().AgentSessionID, nil
}

func (a *app) resumes(id string) bool {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return !a.noResume[id]
}

func (s *session) idleLocked(after time.Duration) bool {
	switch {
	case s.rec.Closed, s.deleted, s.running, len(s.perms) > 0, len(s.queue) > 0:
		return false
	case s.rec.Status != statusDone && s.rec.Status != statusIdle:
		return false
	}
	return time.Since(s.rec.UpdatedAt) >= after
}

func backgroundWork(leader int, procs []proc.Process) bool {
	kids := make(map[int][]proc.Process, len(procs))
	for _, p := range procs {
		kids[p.PPID] = append(kids[p.PPID], p)
	}
	var late func(pid int, since time.Time) bool
	late = func(pid int, since time.Time) bool {
		for _, c := range kids[pid] {
			if isShell(c.Comm) || c.StartTime.After(since) || late(c.PID, since) {
				return true
			}
		}
		return false
	}
	for _, top := range kids[leader] {
		if isShell(top.Comm) || late(top.PID, top.StartTime.Add(startupGrace)) {
			return true
		}
	}
	return false
}

func isShell(comm string) bool {
	return slices.Contains(shells, strings.TrimPrefix(filepath.Base(comm), "-"))
}

func suspendDelay() time.Duration {
	v := os.Getenv(suspendAfterEnv)
	if v == "" {
		return suspendAfter
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents:", suspendAfterEnv+":", err)
		return suspendAfter
	}
	return d
}
