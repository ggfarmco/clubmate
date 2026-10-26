package proc

import (
	"slices"
	"strings"
	"sync"
	"time"
)

const sessionEnvPrefix = "CLUBMATE_SESSION_ID="

type SessionRoot struct {
	SessionID string
	RootPID   int
	PGID      int
}

type Node struct {
	Process   Process `json:"process"`
	SessionID string  `json:"sessionId"`
	Orphan    bool    `json:"orphan"`
	Duplicate bool    `json:"duplicate"`
}

type procKey struct {
	pid   int
	start time.Time
}

type Tracker struct {
	readEnv func(pid int) ([]string, error)
	self    map[int]bool

	mu       sync.Mutex
	sessions map[procKey]string
	markers  map[procKey]string
}

func NewTracker(readEnv func(pid int) ([]string, error), self ...int) *Tracker {
	selfSet := make(map[int]bool, len(self))
	for _, pid := range self {
		selfSet[pid] = true
	}
	return &Tracker{
		readEnv:  readEnv,
		self:     selfSet,
		sessions: make(map[procKey]string),
		markers:  make(map[procKey]string),
	}
}

func (t *Tracker) Attribute(procs []Process, roots []SessionRoot) []Node {
	t.mu.Lock()
	defer t.mu.Unlock()

	rootByPGID := make(map[int]string, len(roots))
	rootIDs := make(map[string]bool, len(roots))
	for _, r := range roots {
		rootByPGID[r.PGID] = r.SessionID
		rootIDs[r.SessionID] = true
	}

	present := make(map[procKey]bool, len(procs))
	owner := make(map[int]string)
	for _, p := range procs {
		key := keyOf(p)
		present[key] = true
		if t.self[p.PID] {
			continue
		}
		if id, ok := t.directSession(key, p, rootByPGID, rootIDs); ok {
			owner[p.PID] = id
		}
	}

	for changed := true; changed; {
		changed = false
		for _, p := range procs {
			if t.self[p.PID] {
				continue
			}
			if _, ok := owner[p.PID]; ok {
				continue
			}
			if id, ok := owner[p.PPID]; ok {
				owner[p.PID] = id
				changed = true
			}
		}
	}

	nodes := t.collect(procs, owner)
	t.prune(present)
	markDuplicates(nodes)
	sortNodes(nodes)
	return nodes
}

func (t *Tracker) directSession(key procKey, p Process, rootByPGID map[int]string, rootIDs map[string]bool) (string, bool) {
	if id, ok := t.sessions[key]; ok {
		return id, true
	}
	if id, ok := rootByPGID[p.PGID]; ok {
		return id, true
	}
	if id := t.marker(key, p.PID); rootIDs[id] {
		return id, true
	}
	return "", false
}

func (t *Tracker) marker(key procKey, pid int) string {
	if id, ok := t.markers[key]; ok {
		return id
	}
	id := ""
	if env, err := t.readEnv(pid); err == nil {
		id = markerFrom(env)
	}
	t.markers[key] = id
	return id
}

func (t *Tracker) collect(procs []Process, owner map[int]string) []Node {
	var nodes []Node
	for _, p := range procs {
		id, ok := owner[p.PID]
		if !ok {
			continue
		}
		t.sessions[keyOf(p)] = id
		nodes = append(nodes, Node{Process: p, SessionID: id, Orphan: p.PPID == 1})
	}
	return nodes
}

func (t *Tracker) prune(present map[procKey]bool) {
	for key := range t.sessions {
		if !present[key] {
			delete(t.sessions, key)
		}
	}
	for key := range t.markers {
		if !present[key] {
			delete(t.markers, key)
		}
	}
}

func markDuplicates(nodes []Node) {
	type commKey struct{ session, comm string }
	counts := make(map[commKey]int, len(nodes))
	for _, n := range nodes {
		counts[commKey{n.SessionID, n.Process.Comm}]++
	}
	for i := range nodes {
		nodes[i].Duplicate = counts[commKey{nodes[i].SessionID, nodes[i].Process.Comm}] > 1
	}
}

func sortNodes(nodes []Node) {
	slices.SortFunc(nodes, func(a, b Node) int {
		if c := strings.Compare(a.SessionID, b.SessionID); c != 0 {
			return c
		}
		return a.Process.PID - b.Process.PID
	})
}

func markerFrom(env []string) string {
	for _, e := range env {
		if id, ok := strings.CutPrefix(e, sessionEnvPrefix); ok {
			return id
		}
	}
	return ""
}

func keyOf(p Process) procKey {
	return procKey{pid: p.PID, start: p.StartTime}
}
