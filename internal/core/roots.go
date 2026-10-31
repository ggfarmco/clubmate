package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/internal/id"
	"clubmate/internal/proc"
)

const (
	RootIDPrefix  = "prc"
	maxLabelRunes = 200
	exitedSuffix  = " (exited)"
)

var (
	ErrNotOwned     = errors.New("process is not a child of the caller")
	ErrInvalidLabel = errors.New("label must be 1..200 characters")
	ErrOwnerClosed  = errors.New("caller's connection is closed")
)

type RootOwner struct {
	Plugin string
	PID    int
}

type RegisterRequest struct {
	ProjectID string `json:"projectId"`
	PID       int    `json:"pid"`
	Label     string `json:"label"`
}

type ProcessRoot struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Label     string `json:"label"`
	Owner     string `json:"owner"`
	PID       int    `json:"pid"`
	Ended     bool   `json:"ended"`
}

type rootRecord struct {
	ProcessRoot
	ownerPID   int
	start      time.Time
	registered time.Time
	dead       bool
	orphaned   bool
	reaped     bool
}

func (r *rootRecord) view() ProcessRoot {
	v := r.ProcessRoot
	if r.dead {
		v.Label += exitedSuffix
	}
	return v
}

func (s *Service) RegisterProcess(ctx context.Context, owner RootOwner, r RegisterRequest) (ProcessRoot, error) {
	label := strings.TrimSpace(r.Label)
	if label == "" || utf8.RuneCountInString(label) > maxLabelRunes {
		return ProcessRoot{}, ErrInvalidLabel
	}
	if _, err := s.projects.Get(ctx, r.ProjectID); err != nil {
		return ProcessRoot{}, err
	}
	procs, err := s.snapshot()
	if err != nil {
		return ProcessRoot{}, err
	}
	p, ok := ownedBy(procs, owner.PID, r.PID)
	if !ok {
		return ProcessRoot{}, fmt.Errorf("pid %d: %w", r.PID, ErrNotOwned)
	}
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	if s.dropped[owner] {
		return ProcessRoot{}, ErrOwnerClosed
	}
	i := slices.IndexFunc(s.roots, func(x *rootRecord) bool { return x.PID == r.PID && x.start.Equal(p.StartTime) })
	if i >= 0 && s.roots[i].ownerPID != owner.PID {
		return ProcessRoot{}, fmt.Errorf("pid %d: %w", r.PID, ErrNotOwned)
	}
	if i < 0 {
		s.roots = append(s.roots, &rootRecord{ProcessRoot: ProcessRoot{ID: id.New(RootIDPrefix), Owner: owner.Plugin, PID: r.PID}, ownerPID: owner.PID, start: p.StartTime})
		i = len(s.roots) - 1
	}
	rec := s.roots[i]
	rec.ProjectID, rec.Label, rec.Ended, rec.registered = r.ProjectID, label, false, time.Now()
	return rec.view(), nil
}

func (s *Service) UnregisterProcess(owner RootOwner, pid int) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	for _, r := range s.roots {
		if r.ownerPID == owner.PID && r.Owner == owner.Plugin && r.PID == pid {
			r.Ended = true
		}
	}
}

func (s *Service) DropProcesses(owner RootOwner) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.dropped[owner] = true
	for _, r := range s.roots {
		if r.ownerPID == owner.PID && r.Owner == owner.Plugin {
			r.Ended = true
		}
	}
}

func (s *Service) ProcessRoots(projectID string) []ProcessRoot {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	var out []ProcessRoot
	for _, r := range s.roots {
		if !r.dead && !s.alive(r) {
			r.Ended, r.dead = true, true
		}
		if projectID == "" || r.ProjectID == projectID {
			out = append(out, r.view())
		}
	}
	return out
}

func (s *Service) registeredRoots(byPID map[int]proc.Process, snapStart time.Time, projectOf map[string]string) []proc.SessionRoot {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	out := make([]proc.SessionRoot, 0, len(s.roots))
	for _, r := range s.roots {
		p, ok := byPID[r.PID]
		if !(ok && p.StartTime.Equal(r.start)) && r.registered.Before(snapStart) && !s.alive(r) {
			r.Ended, r.dead = true, true
		}
		projectOf[r.ID] = r.ProjectID
		root := proc.SessionRoot{SessionID: r.ID}
		if !r.Ended {
			root.RootPID, root.Owned = r.PID, true
		}
		out = append(out, root)
	}
	return out
}

func (s *Service) sweepRoots(ctx context.Context, nodes []proc.Node) {
	carried := make(map[string]bool)
	for _, n := range nodes {
		carried[n.SessionID] = true
	}
	s.rootsMu.Lock()
	reap := make(map[string]bool)
	for _, r := range s.roots {
		if r.Ended && r.orphaned && !r.reaped && carried[r.ID] {
			r.reaped, reap[r.ID] = true, true
		}
	}
	s.roots = slices.DeleteFunc(s.roots, func(r *rootRecord) bool { return r.Ended && !carried[r.ID] })
	s.rootsMu.Unlock()
	if len(reap) == 0 {
		return
	}
	leftovers := slices.DeleteFunc(slices.Clone(nodes), func(n proc.Node) bool { return !reap[n.SessionID] })
	go func() {
		if _, err := proc.KillTree(context.WithoutCancel(ctx), nodeTargets(leftovers), s.killGrace); err != nil {
			s.logger.Error("kill leftovers of a deleted workspace", "err", err)
		}
	}()
}

func (s *Service) alive(r *rootRecord) bool {
	start, err := s.startTime(r.PID)
	return err == nil && start.Equal(r.start)
}

func (s *Service) withoutLiveRoots(projectID string, nodes []proc.Node) []proc.Node {
	s.rootsMu.Lock()
	live := make(map[string]bool, len(s.roots))
	for _, r := range s.roots {
		if r.ProjectID == projectID {
			r.orphaned = true
		}
		if !r.Ended {
			live[r.ID] = true
		}
	}
	s.rootsMu.Unlock()
	return slices.DeleteFunc(nodes, func(n proc.Node) bool { return live[n.SessionID] })
}

func ownedBy(procs []proc.Process, ownerPID, pid int) (proc.Process, bool) {
	if pid <= 1 || ownerPID <= 1 || pid == ownerPID {
		return proc.Process{}, false
	}
	byPID := make(map[int]proc.Process, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	target, ok := byPID[pid]
	for p, hops := target, 0; ok && p.PPID > 1 && hops < len(procs); hops++ {
		if p.PPID == ownerPID {
			return target, true
		}
		p, ok = byPID[p.PPID]
	}
	return proc.Process{}, false
}
