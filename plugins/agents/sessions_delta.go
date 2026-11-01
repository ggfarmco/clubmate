package main

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"sync"
)

type sessionRevs struct {
	mu   sync.Mutex
	rev  int64
	seen map[string]map[string]sessionRev
}

type sessionRev struct {
	sum     uint64
	rev     int64
	removed bool
}

type sessionsPage struct {
	Sessions []sessionInfo `json:"sessions"`
	Removed  []string      `json:"removed"`
	Cursor   string        `json:"cursor"`
	Delta    bool          `json:"delta,omitempty"`
}

func (r *sessionRevs) page(workspaceID string, infos []sessionInfo, since string) sessionsPage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]map[string]sessionRev{}
	}
	seen := r.seen[workspaceID]
	if seen == nil {
		seen = map[string]sessionRev{}
		r.seen[workspaceID] = seen
	}
	live := make(map[string]bool, len(infos))
	for _, info := range infos {
		live[info.ID] = true
		sum := infoSum(info)
		if old, ok := seen[info.ID]; !ok || old.removed || old.sum != sum {
			r.rev++
			seen[info.ID] = sessionRev{sum: sum, rev: r.rev}
		}
	}
	for id, old := range seen {
		if !live[id] && !old.removed {
			r.rev++
			seen[id] = sessionRev{rev: r.rev, removed: true}
		}
	}
	out := sessionsPage{Sessions: infos, Removed: []string{}, Cursor: transcriptEpoch + cursorSep + strconv.FormatInt(r.rev, 10)}
	after, ok := parseCursor(since)
	if !ok || after > r.rev {
		return out
	}
	out.Delta = true
	out.Sessions = slices.DeleteFunc(infos, func(info sessionInfo) bool { return seen[info.ID].rev <= after })
	for id, s := range seen {
		if s.removed && s.rev > after {
			out.Removed = append(out.Removed, id)
		}
	}
	slices.Sort(out.Removed)
	return out
}

func infoSum(info sessionInfo) uint64 {
	h := fnv.New64a()
	if err := json.NewEncoder(h).Encode(info); err != nil {
		return 0
	}
	return h.Sum64()
}
