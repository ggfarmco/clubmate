package main

import (
	"iter"
	"slices"

	"clubmate/plugins/agents/api"
)

type lineage struct {
	ParentSession string `json:"parentSession,omitempty"`
	Advisor       string `json:"advisor,omitempty"`
	Fork          string `json:"fork,omitempty"`
	ForkBranch    string `json:"forkBranch,omitempty"`
	HandoffFrom   string `json:"handoffFrom,omitempty"`
	HandoffTo     string `json:"handoffTo,omitempty"`
	ImportedFrom  string `json:"importedFrom,omitempty"`
}

func (a *app) children(id string) []*session {
	out := a.roster.sessions(a, func(rec *record) bool { return rec.ParentSession == id && !rec.isAside() })
	slices.SortFunc(out, func(x, y *session) int { return x.snapshot().CreatedAt.Compare(y.snapshot().CreatedAt) })
	return out
}

func (a *app) asides(id string) []*session {
	out := a.roster.sessions(a, func(rec *record) bool { return rec.ParentSession == id && rec.isAside() })
	slices.SortFunc(out, func(x, y *session) int { return y.snapshot().CreatedAt.Compare(x.snapshot().CreatedAt) })
	return out
}

func (a *app) asksParent(s *session, rec record) bool {
	if rec.Status != statusWaiting || rec.ParentSession == "" || rec.isAside() || !s.onlyQuestions() {
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
		if turnActive(c.snapshot()) {
			n++
		}
	}
	return n
}

func (a *app) limitChildren(id string) int {
	n := 0
	for _, c := range a.children(id) {
		if rec := c.snapshot(); turnActive(rec) && (rec.Held == nil || c.working()) {
			n++
		}
	}
	return n
}

func (a *app) openStartedBy(starter string) int {
	n := 0
	for _, s := range a.allSessions() {
		if rec := s.snapshot(); rec.StartedBy == starter && !rec.closed() {
			n++
		}
	}
	return n
}

func (a *app) descendantOf(id, ancestor string) bool {
	s := a.session(id)
	if s == nil || id == ancestor || s.snapshot().isAside() {
		return false
	}
	for rec := range a.ancestry(s.snapshot()) {
		if rec.ID == ancestor {
			return true
		}
	}
	return false
}

func (a *app) userStarted(rec record) bool {
	for r := range a.ancestry(rec) {
		if r.CronJob != "" || r.Trigger != "" || r.StartedBy != "" || r.Run != nil {
			return false
		}
		rec = r
	}
	return rec.ParentSession == ""
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
	return t.Status == statusDone && !a.activeBelow(t.Workspace, t.Session)
}

func (a *app) activeBelow(workspace, id string) bool {
	return activeDescendant(id, statusKids(a.list(workspace), a.gateRunOpen), map[string]bool{}) != ""
}

func statusParent(rec record) string {
	if gf := rec.GateFor; rec.ParentSession == "" && gf.open() {
		return gf.Lander
	}
	return rec.ParentSession
}

func statusKids(recs []record, live func(string) bool) map[string][]record {
	kids := map[string][]record{}
	for _, rec := range recs {
		if p := statusParent(rec); p != "" && !rec.isAside() {
			kids[p] = append(kids[p], rec)
		}
		if run := rec.reviewRun(live); run != "" {
			kids[rec.ID] = append(kids[rec.ID], record{ID: run, Status: statusRunning})
		}
	}
	return kids
}

func kidsOf(recs []record) map[string][]record {
	kids := map[string][]record{}
	for _, rec := range recs {
		if rec.ParentSession != "" && !rec.isAside() {
			kids[rec.ParentSession] = append(kids[rec.ParentSession], rec)
		}
	}
	return kids
}

func childStatuses(recs []record, live func(string) bool) map[string]string {
	kids := statusKids(recs, live)
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
