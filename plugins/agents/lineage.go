package main

import (
	"iter"
	"maps"
	"slices"

	"clubmate/plugins/agents/api"
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

func (a *app) asksParent(s *session, rec record) bool {
	if rec.Status != statusWaiting || rec.ParentSession == "" || !s.onlyQuestions() {
		return false
	}
	parent := a.session(rec.ParentSession)
	return parent != nil && !parent.snapshot().closed()
}

func (a *app) subtree(s *session) []*session {
	out := []*session{s}
	for i := 0; i < len(out); i++ {
		out = append(out, a.children(out[i].snapshot().ID)...)
	}
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

func (a *app) finished(t api.Turn) bool {
	return t.Status == statusDone && activeDescendant(t.Session, kidsOf(a.list(t.Workspace)), map[string]bool{}) == ""
}

func kidsOf(recs []record) map[string][]record {
	kids := map[string][]record{}
	for _, rec := range recs {
		if rec.ParentSession != "" {
			kids[rec.ParentSession] = append(kids[rec.ParentSession], rec)
		}
	}
	return kids
}

func childStatuses(recs []record) map[string]string {
	kids := kidsOf(recs)
	out := map[string]string{}
	for _, rec := range recs {
		if rec.Status != statusDone && rec.Status != statusIdle || needsYou(rec) {
			continue
		}
		if st := activeDescendant(rec.ID, kids, map[string]bool{}); st != "" {
			out[rec.ID] = st
		}
	}
	return out
}

func activeDescendant(id string, kids map[string][]record, seen map[string]bool) string {
	seen[id] = true
	found := ""
	for _, kid := range kids[id] {
		if !seen[kid.ID] {
			status := kid.Status
			if kid.AsksParent {
				status = statusRunning
			}
			found = moreUrgent(found, moreUrgent(status, activeDescendant(kid.ID, kids, seen)))
		}
	}
	return found
}

func moreUrgent(a, b string) string {
	switch {
	case a == statusWaiting || b == statusWaiting:
		return statusWaiting
	case a == statusRunning || b == statusRunning:
		return statusRunning
	}
	return ""
}
