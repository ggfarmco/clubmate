package core

import (
	"sync"
	"time"

	"clubmate/internal/proc"
)

type procCache struct {
	ttl      time.Duration
	list     func() ([]proc.Process, error)
	readArgs func(pid int) []string

	mu    sync.Mutex
	at    time.Time
	table procTable

	argsMu sync.Mutex
	args   map[argsKey][]string
}

type procTable struct {
	procs []proc.Process
	byPID map[int]proc.Process
}

type argsKey struct {
	pid   int
	start int64
	comm  string
}

func WithSnapshotTTL(d time.Duration) Option {
	return func(s *Service) { s.cache.ttl = d }
}

func WithProcessList(fn func() ([]proc.Process, error)) Option {
	return func(s *Service) { s.cache.list = fn }
}

func WithArgsReader(fn func(pid int) []string) Option {
	return func(s *Service) { s.cache.readArgs = fn }
}

func (s *Service) ProcessArgs(p proc.Process) []string {
	c := &s.cache
	key := argsKey{pid: p.PID, start: p.StartTime.UnixNano(), comm: p.Comm}
	c.argsMu.Lock()
	args, ok := c.args[key]
	c.argsMu.Unlock()
	if ok {
		return args
	}
	args = c.readArgs(p.PID)
	if len(args) == 0 {
		return args
	}
	c.argsMu.Lock()
	c.args[key] = args
	c.argsMu.Unlock()
	return args
}

func (s *Service) cachedSnapshot() (procTable, error) {
	c := &s.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.table.byPID != nil && time.Since(c.at) < c.ttl {
		return c.table, nil
	}
	return s.snapshotLocked()
}

func (s *Service) freshSnapshot() (procTable, error) {
	procs, err := s.cache.list()
	if err != nil {
		return procTable{}, err
	}
	t := newProcTable(procs)
	s.pruneArgs(t.byPID)
	return t, nil
}

func (s *Service) snapshotLocked() (procTable, error) {
	procs, err := s.snapshot()
	if err != nil {
		return procTable{}, err
	}
	t := newProcTable(procs)
	if s.cache.ttl > 0 {
		s.cache.table, s.cache.at = t, time.Now()
	}
	s.pruneArgs(t.byPID)
	return t, nil
}

func (s *Service) pruneArgs(byPID map[int]proc.Process) {
	c := &s.cache
	c.argsMu.Lock()
	defer c.argsMu.Unlock()
	for key := range c.args {
		if p, ok := byPID[key.pid]; !ok || p.StartTime.UnixNano() != key.start {
			delete(c.args, key)
		}
	}
}

func newProcTable(procs []proc.Process) procTable {
	t := procTable{procs: procs, byPID: make(map[int]proc.Process, len(procs))}
	for _, p := range procs {
		t.byPID[p.PID] = p
	}
	return t
}
