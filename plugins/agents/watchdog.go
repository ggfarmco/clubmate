package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	wakeDelay    = time.Second
	wakeDelayEnv = "CLUBMATE_AGENTS_WAKE_DELAY"
	cutOffText   = "[Clubmate] Your turn was cut off by a restart; continue from where you stopped."
	childFormat  = "[Clubmate] Child %q (%s) is %s."
	askFormat    = "[Clubmate] Child %q (%s) asks you, not the user: %q (agents_elicitation request %s). Answer it yourself; ask the user only when the decision is theirs."
)

type waitReg struct {
	caller string
	ids    []string
}

type watchdog struct {
	mu      sync.Mutex
	last    map[string]string
	seen    map[string]string
	pending map[string]map[string]string
	waits   map[string][]*waitReg
	handing map[string]bool
	timers  map[string]*time.Timer
	delay   time.Duration

	retries     map[string]int
	retryTimers map[string]*time.Timer
}

func newWatchdog() watchdog {
	return watchdog{last: map[string]string{}, seen: map[string]string{}, pending: map[string]map[string]string{}, waits: map[string][]*waitReg{}, handing: map[string]bool{}, timers: map[string]*time.Timer{}, delay: envDelay(wakeDelayEnv, wakeDelay), retries: map[string]int{}, retryTimers: map[string]*time.Timer{}}
}

func (a *app) continueCutOff(cut map[*session]string) {
	for s, text := range cut {
		a.runIdle(s, promptArgs{Text: text}, (*session).requeueLocked)
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
	key := progressKey(turn, rec.Status, s.pending())
	w := &a.watchdog
	w.mu.Lock()
	prev := w.last[rec.ID]
	w.last[rec.ID] = rec.Status
	report := rec.ParentSession != "" && reportable(rec.Status) && busy(prev) && prev != rec.Status && w.seen[rec.ID] != key && w.retryTimers[rec.ID] == nil
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
	reg := &waitReg{caller: caller, ids: ids}
	w.mu.Lock()
	w.waits[caller] = append(w.waits[caller], reg)
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		caller := reg.caller
		w.waits[caller] = slices.DeleteFunc(w.waits[caller], func(r *waitReg) bool { return r == reg })
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

func (w *watchdog) waitingLocked(caller, id string) bool {
	return slices.ContainsFunc(w.waits[caller], func(r *waitReg) bool { return slices.Contains(r.ids, id) })
}

func (a *app) beginHandoff(from string) {
	w := &a.watchdog
	w.mu.Lock()
	w.handing[from] = true
	w.mu.Unlock()
}

func (a *app) endHandoff(from, to string) {
	w := &a.watchdog
	w.mu.Lock()
	delete(w.handing, from)
	for _, reg := range w.waits[from] {
		reg.caller = to
	}
	w.waits[to] = append(w.waits[to], w.waits[from]...)
	delete(w.waits, from)
	if len(w.waits[to]) == 0 {
		delete(w.waits, to)
	}
	taken := w.pending[from]
	delete(w.pending, from)
	w.mu.Unlock()
	a.giveWakes(to, taken)
}

func (a *app) observed(caller string, out []waitResult) {
	keys := map[string]string{}
	for _, r := range out {
		if s := a.session(r.Session); s != nil && s.snapshot().ParentSession == caller {
			keys[r.Session] = progressKey(r.Turn, r.Status, s.pending())
		}
	}
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	for kid, key := range keys {
		w.seen[kid] = key
		if w.pending[caller][kid] == key {
			delete(w.pending[caller], kid)
		}
	}
}

func (a *app) scheduleWake(id string) {
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timers[id] == nil {
		w.timers[id] = time.AfterFunc(w.delay, func() { a.flushWake(id) })
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
	case rec.closed() && rec.HandoffTo != "":
		a.giveWakes(rec.HandoffTo, a.takeWakes(id))
		return
	case rec.closed():
		if !a.handingOff(id) {
			a.dropWakes(id)
		}
		return
	case running || busy(rec.Status):
		return
	}
	w.mu.Lock()
	taken := map[string]string{}
	for kid, key := range w.pending[id] {
		if !w.waitingLocked(id, kid) {
			taken[kid] = key
			w.seen[kid] = key
			delete(w.pending[id], kid)
		}
	}
	w.mu.Unlock()
	var lines []string
	for _, kid := range a.children(id) {
		k := kid.snapshot()
		key, ok := taken[k.ID]
		switch {
		case !ok || k.Status == statusRunning:
		case a.asksParent(kid, k):
			if sum, ids := kid.permissionSummary(), kid.pending(); sum != nil && len(ids) > 0 {
				lines = append(lines, fmt.Sprintf(askFormat, childName(k), k.ID, sum.Text, ids[0]))
				break
			}
			fallthrough
		default:
			status := strings.Split(key, "/")[1]
			lines = append(lines, fmt.Sprintf(childFormat, childName(k), k.ID, status))
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

func (a *app) takeWakes(id string) map[string]string {
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	taken := w.pending[id]
	delete(w.pending, id)
	return taken
}

func (a *app) giveWakes(id string, wakes map[string]string) {
	if len(wakes) == 0 {
		return
	}
	w := &a.watchdog
	w.mu.Lock()
	if w.pending[id] == nil {
		w.pending[id] = map[string]string{}
	}
	for kid, key := range wakes {
		if _, newer := w.pending[id][kid]; !newer {
			w.pending[id][kid] = key
		}
	}
	w.mu.Unlock()
	a.scheduleWake(id)
}

func (a *app) handingOff(id string) bool {
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.handing[id]
}

func (a *app) dropWakes(id string) {
	w := &a.watchdog
	w.mu.Lock()
	delete(w.pending, id)
	w.mu.Unlock()
}

func childName(rec record) string {
	if rec.RepoPath != "" {
		return filepath.Base(rec.RepoPath)
	}
	return firstLine(rec.Title)
}

func (a *app) submitIdle(s *session, args promptArgs) bool {
	return a.runIdle(s, args, (*session).enqueueLocked)
}

func (a *app) runIdle(s *session, args promptArgs, add func(*session, promptArgs)) bool {
	s.mu.Lock()
	s.loadLocked()
	if s.running || s.deleted || s.rec.closed() {
		s.mu.Unlock()
		return false
	}
	add(s, args)
	a.runQueuedLocked(s)
	return true
}

func (s *session) progress() (record, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec, s.turn, s.running
}

func progressKey(turn int, status string, pending []string) string {
	key := strconv.Itoa(turn) + "/" + status
	if status == statusWaiting && len(pending) > 0 {
		key += "/" + pending[0]
	}
	return key
}

func reportable(status string) bool {
	return status == statusDone || status == statusError || status == statusWaiting
}

func busy(status string) bool {
	return status == statusRunning || status == statusWaiting
}
