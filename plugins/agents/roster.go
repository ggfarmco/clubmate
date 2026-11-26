package main

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var recordsRev atomic.Uint64

type recLock struct {
	sync.Mutex
	ver atomic.Uint64
}

type roster struct {
	mu      sync.Mutex
	synced  bool
	rev     uint64
	seen    map[string]rosterEntry
	lists   map[string]*rosterList
	shape   uint64
	taken   int
	spend   spendTree
	spendAt uint64
}

type rosterEntry struct {
	s         *session
	ver       uint64
	id        string
	workspace string
	created   time.Time
	parent    string
}

type rosterRow struct {
	key string
	rec record
}

type rosterList struct {
	rows []rosterRow
	at   map[string]int
}

func (l *recLock) Unlock() {
	l.ver.Add(1)
	recordsRev.Add(1)
	l.Mutex.Unlock()
}

func (l *recLock) RLock() {
	l.Mutex.Lock()
}

func (l *recLock) RUnlock() {
	l.Mutex.Unlock()
}

func (s *session) versioned() (record, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rec, s.mu.ver.Load()
}

func (r *roster) sync(a *app) {
	rev := recordsRev.Load()
	a.mu.Lock()
	if r.synced && rev == r.rev && len(a.sessions) == len(r.seen) {
		a.mu.Unlock()
		return
	}
	keys := make([]string, 0, len(a.sessions))
	all := make([]*session, 0, len(a.sessions))
	for key, s := range a.sessions {
		keys, all = append(keys, key), append(all, s)
	}
	a.mu.Unlock()
	if r.seen == nil {
		r.seen, r.lists = map[string]rosterEntry{}, map[string]*rosterList{}
	}
	fresh := map[string]record{}
	rebuild := map[string]bool{}
	for i, s := range all {
		key := keys[i]
		old, known := r.seen[key]
		if known && old.s == s && old.ver == s.mu.ver.Load() {
			continue
		}
		rec, ver := s.versioned()
		r.taken++
		e := rosterEntry{s: s, ver: ver, id: rec.ID, workspace: rec.Workspace, created: rec.CreatedAt, parent: spendParent(rec)}
		r.seen[key] = e
		if known && old.id == e.id && old.workspace == e.workspace && old.created.Equal(e.created) {
			l := r.lists[e.workspace]
			l.rows[l.at[key]].rec = rec
			if old.parent != e.parent {
				r.shape++
			}
			continue
		}
		if known {
			rebuild[old.workspace] = true
		}
		rebuild[e.workspace], fresh[key] = true, rec
	}
	if len(r.seen) != len(all) {
		live := make(map[string]bool, len(keys))
		for _, key := range keys {
			live[key] = true
		}
		for key, e := range r.seen {
			if !live[key] {
				rebuild[e.workspace] = true
				delete(r.seen, key)
			}
		}
	}
	for workspace := range rebuild {
		r.rebuild(workspace, fresh)
	}
	if len(rebuild) > 0 {
		r.shape++
	}
	r.synced, r.rev = true, rev
}

func (r *roster) rebuild(workspace string, fresh map[string]record) {
	var rows []rosterRow
	if l := r.lists[workspace]; l != nil {
		for _, row := range l.rows {
			if _, again := fresh[row.key]; !again && r.seen[row.key].workspace == workspace && r.seen[row.key].s != nil {
				rows = append(rows, row)
			}
		}
	}
	for key, rec := range fresh {
		if rec.Workspace == workspace {
			rows = append(rows, rosterRow{key: key, rec: rec})
		}
	}
	if len(rows) == 0 {
		delete(r.lists, workspace)
		return
	}
	slices.SortFunc(rows, func(x, y rosterRow) int { return startedFirst(x.rec, y.rec) })
	at := make(map[string]int, len(rows))
	for i, row := range rows {
		at[row.key] = i
	}
	r.lists[workspace] = &rosterList{rows: rows, at: at}
}

func (r *roster) records(a *app, workspace string, keep func(*record) bool) []record {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sync(a)
	out := []record{}
	l := r.lists[workspace]
	if l == nil {
		return out
	}
	if keep == nil {
		out = make([]record, len(l.rows))
		for i := range l.rows {
			out[i] = l.rows[i].rec
		}
		return out
	}
	for i := range l.rows {
		if keep(&l.rows[i].rec) {
			out = append(out, l.rows[i].rec)
		}
	}
	return out
}

func (r *roster) each(a *app, visit func(*record)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sync(a)
	for _, l := range r.lists {
		for i := range l.rows {
			visit(&l.rows[i].rec)
		}
	}
}

func (r *roster) sessions(a *app, keep func(*record) bool) []*session {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sync(a)
	var out []*session
	for _, l := range r.lists {
		for i := range l.rows {
			if keep(&l.rows[i].rec) {
				out = append(out, r.seen[l.rows[i].key].s)
			}
		}
	}
	return out
}

func (r *roster) spendTree(a *app) spendTree {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sync(a)
	if r.spend != nil && r.spendAt == r.shape {
		return r.spend
	}
	kids := map[string][]string{}
	for _, e := range r.seen {
		if e.parent != "" && e.parent != e.id {
			kids[e.parent] = append(kids[e.parent], e.id)
		}
	}
	r.spend, r.spendAt = spendTreeFrom(kids), r.shape
	return r.spend
}

func (a *app) openRecords() []record {
	var out []record
	a.roster.each(a, func(rec *record) {
		if !rec.closed() {
			out = append(out, *rec)
		}
	})
	return out
}

func (a *app) openRecordsIn(workspaceID string) []record {
	return a.roster.records(a, workspaceID, func(rec *record) bool { return !rec.closed() })
}
