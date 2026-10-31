package main

import (
	"iter"
	"maps"
	"slices"
)

type lineage struct {
	ParentSession string `json:"parentSession,omitempty"`
	Advisor       string `json:"advisor,omitempty"`
	Fork          string `json:"fork,omitempty"`
	HandoffFrom   string `json:"handoffFrom,omitempty"`
	HandoffTo     string `json:"handoffTo,omitempty"`
	ImportedFrom  string `json:"importedFrom,omitempty"`
}

func (a *app) children(id string) []*session {
	a.mu.Lock()
	all := slices.Collect(maps.Values(a.sessions))
	a.mu.Unlock()
	var out []*session
	for _, s := range all {
		if s.snapshot().ParentSession == id {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(x, y *session) int { return x.snapshot().CreatedAt.Compare(y.snapshot().CreatedAt) })
	return out
}

func (a *app) runningChildren(id string) int {
	n := 0
	for _, c := range a.children(id) {
		if st := c.snapshot().Status; st == statusRunning || st == statusWaiting {
			n++
		}
	}
	return n
}

func (a *app) descendantOf(id, ancestor string) bool {
	s := a.session(id)
	if s == nil || id == ancestor {
		return false
	}
	for rec := range a.ancestry(s.snapshot()) {
		if rec.ID == ancestor {
			return true
		}
	}
	return false
}

func (a *app) ancestry(rec record) iter.Seq[record] {
	return func(yield func(record) bool) {
		seen := map[string]bool{}
		for !seen[rec.ID] {
			if !yield(rec) {
				return
			}
			seen[rec.ID] = true
			parent := a.session(rec.ParentSession)
			if parent == nil {
				return
			}
			rec = parent.snapshot()
		}
	}
}
