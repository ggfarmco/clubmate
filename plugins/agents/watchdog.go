package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	wakeDelay    = time.Second
	cutOffText   = "[Clubmate] Your turn was cut off by a restart; continue from where you stopped."
	cutOffNotice = "Turn cut off by a restart"
	cutOffHint   = "send a message to continue"
	childFormat  = "[Clubmate] Child %q (%s) is %s."
)

type watchdog struct {
	mu      sync.Mutex
	last    map[string]string
	seen    map[string]string
	pending map[string]map[string]string
	waits   map[string]map[string]int
	timers  map[string]*time.Timer
}

func newWatchdog() watchdog {
	return watchdog{last: map[string]string{}, seen: map[string]string{}, pending: map[string]map[string]string{}, waits: map[string]map[string]int{}, timers: map[string]*time.Timer{}}
}

func (a *app) continueCutOff(cut []*session) {
	for _, s := range cut {
		rec := s.snapshot()
		if a.effectiveAFK(rec) || a.orchestrated(rec) {
			a.submitIdle(s, promptArgs{Text: cutOffText})
			continue
		}
		s.notice(cutOffNotice, cutOffHint)
	}
}

func (a *app) orchestrated(rec record) bool {
	if p := a.session(rec.ParentSession); p != nil && !p.snapshot().closed() {
		return true
	}
	return slices.ContainsFunc(a.children(rec.ID), func(c *session) bool { return !c.snapshot().closed() })
}

func (a *app) statusChanged(s *session) {
	rec, turn, running := s.progress()
	key := progressKey(turn, rec.Status)
	w := &a.watchdog
	w.mu.Lock()
	prev := w.last[rec.ID]
	w.last[rec.ID] = rec.Status
	report := rec.ParentSession != "" && reportable(rec.Status) && busy(prev) && prev != rec.Status && w.seen[rec.ID] != key
	if report {
		if w.pending[rec.ParentSession] == nil {
			w.pending[rec.ParentSession] = map[string]string{}
		}
		w.pending[rec.ParentSession][rec.ID] = key
	}
	flush := !running && !busy(rec.Status) && len(w.pending[rec.ID]) > 0
	w.mu.Unlock()
	if report {
		a.scheduleWake(rec.ParentSession)
	}
	if flush {
		a.scheduleWake(rec.ID)
	}
}

func (a *app) watchWait(caller string, ids []string) func() {
	if caller == "" {
		return func() {}
	}
	w := &a.watchdog
	w.mu.Lock()
	if w.waits[caller] == nil {
		w.waits[caller] = map[string]int{}
	}
	for _, id := range ids {
		w.waits[caller][id]++
	}
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		for _, id := range ids {
			if w.waits[caller][id]--; w.waits[caller][id] == 0 {
				delete(w.waits[caller], id)
			}
		}
		if len(w.waits[caller]) == 0 {
			delete(w.waits, caller)
		}
		left := len(w.pending[caller]) > 0
		w.mu.Unlock()
		if left {
			a.scheduleWake(caller)
		}
	}
}

func (a *app) observed(caller string, out []waitResult) {
	var kids []waitResult
	for _, r := range out {
		if s := a.session(r.Session); s != nil && s.snapshot().ParentSession == caller {
			kids = append(kids, r)
		}
	}
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range kids {
		key := progressKey(r.Turn, r.Status)
		w.seen[r.Session] = key
		if w.pending[caller][r.Session] == key {
			delete(w.pending[caller], r.Session)
		}
	}
}

func (a *app) scheduleWake(id string) {
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timers[id] == nil {
		w.timers[id] = time.AfterFunc(wakeDelay, func() { a.flushWake(id) })
	}
}

func (a *app) flushWake(id string) {
	w := &a.watchdog
	w.mu.Lock()
	delete(w.timers, id)
	w.mu.Unlock()
	select {
	case <-a.quit:
		return
	default:
	}
	parent := a.session(id)
	if parent == nil {
		a.dropWakes(id)
		return
	}
	rec, _, running := parent.progress()
	switch {
	case rec.closed():
		a.dropWakes(id)
		return
	case running || busy(rec.Status):
		return
	}
	w.mu.Lock()
	taken := map[string]string{}
	for kid, key := range w.pending[id] {
		if w.waits[id][kid] == 0 {
			taken[kid] = key
			w.seen[kid] = key
			delete(w.pending[id], kid)
		}
	}
	w.mu.Unlock()
	var lines []string
	for _, kid := range a.children(id) {
		k := kid.snapshot()
		if key, ok := taken[k.ID]; ok && k.Status != statusRunning {
			_, status, _ := strings.Cut(key, "/")
			lines = append(lines, fmt.Sprintf(childFormat, firstLine(k.Title), k.ID, status))
		}
	}
	if len(lines) == 0 || a.submitIdle(parent, promptArgs{Text: strings.Join(lines, "\n")}) {
		return
	}
	w.mu.Lock()
	for kid, key := range taken {
		if w.pending[id] == nil {
			w.pending[id] = map[string]string{}
		}
		if _, newer := w.pending[id][kid]; !newer {
			w.pending[id][kid] = key
		}
	}
	w.mu.Unlock()
}

func (a *app) dropWakes(id string) {
	w := &a.watchdog
	w.mu.Lock()
	delete(w.pending, id)
	w.mu.Unlock()
}

func (a *app) submitIdle(s *session, args promptArgs) bool {
	s.mu.Lock()
	s.loadLocked()
	if s.running || s.deleted || s.rec.closed() {
		s.mu.Unlock()
		return false
	}
	s.enqueueLocked(args)
	a.runQueuedLocked(s)
	return true
}

func (s *session) progress() (record, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec, s.turn, s.running
}

func progressKey(turn int, status string) string {
	return strconv.Itoa(turn) + "/" + status
}

func reportable(status string) bool {
	return status == statusDone || status == statusError || status == statusWaiting
}

func busy(status string) bool {
	return status == statusRunning || status == statusWaiting
}
