package main

import (
	"cmp"
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"sync"
)

const keptRemoved = 64

type sessionRevs struct {
	mu         sync.Mutex
	rev        int64
	workspaces map[string]*workspaceRevs
}

type workspaceRevs struct {
	seen  map[string]sessionRev
	floor int64
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
	if r.workspaces == nil {
		r.workspaces = map[string]*workspaceRevs{}
	}
	w := r.workspaces[workspaceID]
	if w == nil {
		w = &workspaceRevs{seen: map[string]sessionRev{}}
		r.workspaces[workspaceID] = w
	}
	seen := w.seen
	live := make(map[string]bool, len(infos))
	for _, info := range infos {
		live[info.ID] = true
		sum := infoSum(info)
		if old, ok := seen[info.ID]; !ok || old.removed || old.sum != sum {
			r.rev++
			seen[info.ID] = sessionRev{sum: sum, rev: r.rev}
		}
	}
	var removed []string
	for id, old := range seen {
		if !live[id] && !old.removed {
			r.rev++
			seen[id] = sessionRev{rev: r.rev, removed: true}
		}
		if seen[id].removed {
			removed = append(removed, id)
		}
	}
	w.prune(removed)
	out := sessionsPage{Sessions: infos, Removed: []string{}, Cursor: transcriptEpoch + cursorSep + strconv.FormatInt(r.rev, 10)}
	after, ok := parseCursor(since)
	if !ok || after > r.rev || after < w.floor {
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

func (w *workspaceRevs) prune(removed []string) {
	if len(removed) <= keptRemoved {
		return
	}
	slices.SortFunc(removed, func(x, y string) int { return cmp.Compare(w.seen[x].rev, w.seen[y].rev) })
	for _, id := range removed[:len(removed)-keptRemoved] {
		w.floor = max(w.floor, w.seen[id].rev)
		delete(w.seen, id)
	}
}

func infoSum(info sessionInfo) uint64 {
	h := fnv.New64a()
	if err := json.NewEncoder(h).Encode(info); err != nil {
		return 0
	}
	return h.Sum64()
}
