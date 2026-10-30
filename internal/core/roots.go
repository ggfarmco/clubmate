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
)

var (
	ErrNotOwned     = errors.New("process is not a child of the caller")
	ErrInvalidLabel = errors.New("label must be 1..200 characters")
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

	ownerPID int
	start    time.Time
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
	i := slices.IndexFunc(s.roots, func(x ProcessRoot) bool { return x.PID == r.PID && x.start.Equal(p.StartTime) })
	if i < 0 {
		s.roots = append(s.roots, ProcessRoot{ID: id.New(RootIDPrefix), PID: r.PID, start: p.StartTime})
		i = len(s.roots) - 1
	}
	s.roots[i].ProjectID, s.roots[i].Label, s.roots[i].Owner, s.roots[i].ownerPID = r.ProjectID, label, owner.Plugin, owner.PID
	return s.roots[i], nil
}

func (s *Service) UnregisterProcess(owner RootOwner, pid int) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.roots = slices.DeleteFunc(s.roots, func(x ProcessRoot) bool { return x.ownerPID == owner.PID && x.PID == pid })
}

func (s *Service) DropProcesses(owner RootOwner) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.roots = slices.DeleteFunc(s.roots, func(x ProcessRoot) bool { return x.ownerPID == owner.PID && x.Owner == owner.Plugin })
}

func (s *Service) ProcessRoots(projectID string) []ProcessRoot {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	var out []ProcessRoot
	for _, r := range s.roots {
		if projectID == "" || r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out
}

func (s *Service) liveRoots(byPID map[int]proc.Process) []ProcessRoot {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.roots = slices.DeleteFunc(s.roots, func(r ProcessRoot) bool {
		p, ok := byPID[r.PID]
		return !ok || !p.StartTime.Equal(r.start)
	})
	return slices.Clone(s.roots)
}

func sessionNodes(nodes []proc.Node) []proc.Node {
	return slices.DeleteFunc(nodes, func(n proc.Node) bool { return id.Valid(RootIDPrefix, n.SessionID) })
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
	if !ok {
		return proc.Process{}, false
	}
	if o, found := byPID[ownerPID]; found && target.PGID == o.PGID {
		return target, true
	}
	for p, hops := target, 0; ok && p.PPID > 1 && hops < len(procs); hops++ {
		if p.PPID == ownerPID {
			return target, true
		}
		p, ok = byPID[p.PPID]
	}
	return proc.Process{}, false
}
