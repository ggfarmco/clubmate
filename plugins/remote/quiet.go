package main

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	quietPrefix   = "quiet/"
	decidedPrefix = "quiet-decided/"
	outcomeAtMac  = " quiet, at the Mac"
	escalateEvery = 10 * time.Second
	escalateMax   = 10 * time.Minute
	requestGate   = "gate"
)

var (
	checkedPrefixes = []string{agentSessionPrefix, "gt_", "gr_"}
	resolvedKinds   = map[string][]string{
		requestPermission: {"permission"},
		"elicitation":     {"question", "needs-input"},
		requestGate:       {"needs-input"},
	}
)

type quietLog struct {
	kv  kvStore
	now func() time.Time

	mu      sync.Mutex
	decided map[string]quietDecision
	loaded  bool

	entries sync.Mutex
}

type quietDecision struct {
	Here bool      `json:"here"`
	At   time.Time `json:"at"`
}

type quietEntry struct {
	Notification notifyPayload `json:"notification"`
	At           time.Time     `json:"at"`
	Tries        int           `json:"tries,omitempty"`
	Next         time.Time     `json:"next,omitzero"`
}

type sessionRow struct {
	ID              string `json:"id"`
	NotifyEffective string `json:"notifyEffective"`
	NeedsYou        bool   `json:"needsYou"`
}

func newQuietLog(kv kvStore, now func() time.Time) *quietLog {
	return &quietLog{kv: kv, now: now, decided: map[string]quietDecision{}}
}

func (l *quietLog) decide(ctx context.Context, id string, here func() bool) bool {
	if id == "" {
		return here()
	}
	l.load(ctx)
	l.mu.Lock()
	now := l.now()
	var expired []string
	for old, d := range l.decided {
		if now.Sub(d.At) > pushMaxAge {
			delete(l.decided, old)
			expired = append(expired, old)
		}
	}
	d, ok := l.decided[id]
	if !ok {
		d = quietDecision{Here: here(), At: now}
		l.decided[id] = d
	}
	l.mu.Unlock()
	for _, old := range expired {
		if err := l.kv.StorageDelete(ctx, decidedPrefix+old); err != nil {
			logf("drop quiet decision %s: %v", old, err)
		}
	}
	if !ok {
		if err := l.kv.StorageSet(ctx, decidedPrefix+id, d); err != nil {
			logf("save quiet decision %s: %v", id, err)
		}
	}
	return d.Here
}

func (l *quietLog) quieted(ctx context.Context, id string) bool {
	l.load(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	d, ok := l.decided[id]
	return ok && d.Here && l.now().Sub(d.At) <= pushMaxAge
}

func (l *quietLog) load(ctx context.Context) {
	l.mu.Lock()
	loaded := l.loaded
	l.mu.Unlock()
	if loaded {
		return
	}
	keys, err := l.kv.StorageList(ctx, decidedPrefix)
	if err != nil {
		logf("list quiet decisions: %v", err)
		return
	}
	stored := map[string]quietDecision{}
	for _, k := range keys {
		var d quietDecision
		if found, err := l.kv.StorageGet(ctx, k, &d); err == nil && found {
			stored[strings.TrimPrefix(k, decidedPrefix)] = d
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, d := range stored {
		if _, ok := l.decided[id]; !ok {
			l.decided[id] = d
		}
	}
	l.loaded = true
}

func (l *quietLog) hold(ctx context.Context, n notifyPayload) {
	if n.ID == "" || !slices.Contains(needsInputKinds, n.Kind) || !slices.ContainsFunc(checkedPrefixes, func(p string) bool { return strings.HasPrefix(n.Session, p) }) {
		return
	}
	l.save(ctx, quietEntry{Notification: n, At: l.now()})
}

func (l *quietLog) save(ctx context.Context, e quietEntry) {
	if err := l.kv.StorageSet(ctx, quietPrefix+e.Notification.ID, e); err != nil {
		logf("hold quiet notification %s: %v", e.Notification.ID, err)
	}
}

func (l *quietLog) failed(ctx context.Context, e quietEntry) {
	l.entries.Lock()
	defer l.entries.Unlock()
	var cur quietEntry
	if found, err := l.kv.StorageGet(ctx, quietPrefix+e.Notification.ID, &cur); err != nil || !found {
		return
	}
	e.Tries++
	e.Next = l.now().Add(min(escalateEvery<<e.Tries, escalateMax))
	l.save(ctx, e)
}

func (l *quietLog) pending(ctx context.Context) []quietEntry {
	keys, err := l.kv.StorageList(ctx, quietPrefix)
	if err != nil {
		logf("list quiet notifications: %v", err)
		return nil
	}
	var out []quietEntry
	for _, k := range keys {
		var e quietEntry
		if found, err := l.kv.StorageGet(ctx, k, &e); err != nil || !found {
			continue
		}
		if l.now().Sub(e.At) > pushMaxAge {
			l.drop(ctx, strings.TrimPrefix(k, quietPrefix))
			continue
		}
		out = append(out, e)
	}
	return out
}

func (l *quietLog) drop(ctx context.Context, id string) {
	l.entries.Lock()
	defer l.entries.Unlock()
	if err := l.kv.StorageDelete(ctx, quietPrefix+id); err != nil {
		logf("drop quiet notification %s: %v", id, err)
	}
}

func (l *quietLog) resolved(ctx context.Context, c agentsChanged) {
	kinds := resolvedKinds[cmp.Or(c.RequestKind, requestPermission)]
	for _, e := range l.pending(ctx) {
		n := e.Notification
		if n.Session != c.Session || !slices.Contains(kinds, n.Kind) {
			continue
		}
		if c.RequestKind == requestGate && c.Request != "" && n.Request != "" && n.Request != c.Request {
			continue
		}
		l.drop(ctx, n.ID)
	}
}

func (s *server) quietHere(ctx context.Context, n notifyPayload) bool {
	return !n.Quiet && s.quiet.decide(ctx, n.ID, s.presence.atMac)
}

func resolvedChange(ev pluginsdk.Event) (agentsChanged, bool) {
	var c agentsChanged
	if ev.Topic != agentsChangedTopic || json.Unmarshal(ev.Payload, &c) != nil || c.Kind != changeResolved || c.Session == "" {
		return agentsChanged{}, false
	}
	return c, true
}

func (s *server) quietEvent(ctx context.Context, ev pluginsdk.Event) pluginsdk.Event {
	if ev.Topic != notifyTopic {
		return ev
	}
	var n notifyPayload
	if !s.relay.configured() {
		return ev
	}
	if err := json.Unmarshal(ev.Payload, &n); err != nil || !s.quietHere(ctx, n) {
		return ev
	}
	return markPayload(ev, "quiet")
}

func markPayload(ev pluginsdk.Event, keys ...string) pluginsdk.Event {
	if len(keys) == 0 {
		return ev
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return ev
	}
	for _, k := range keys {
		payload[k] = json.RawMessage("true")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ev
	}
	ev.Payload = raw
	return ev
}

func (s *server) escalate(ctx context.Context, enabled bool) {
	list := s.quiet.pending(ctx)
	if len(list) == 0 || s.presence.atMac() {
		return
	}
	newest := map[string]quietEntry{}
	for _, e := range list {
		k := e.Notification.collapseID()
		if cur, ok := newest[k]; !ok || e.At.After(cur.At) {
			newest[k] = e
		}
	}
	for _, e := range list {
		if newest[e.Notification.collapseID()].Notification.ID != e.Notification.ID {
			s.quiet.drop(ctx, e.Notification.ID)
		}
	}
	for _, e := range newest {
		n := e.Notification
		if e.Next.After(s.now()) {
			continue
		}
		if s.held(ctx, n.Workspace, enabled, n) != "" || !s.needsYou(ctx, n) {
			s.quiet.drop(ctx, n.ID)
			continue
		}
		devices, err := s.sendAlert(ctx, n.Workspace, n, false, true)
		if err == nil && slices.ContainsFunc(devices, func(d devicePush) bool { return d.Outcome == strconv.Itoa(http.StatusOK) }) {
			s.quiet.drop(ctx, n.ID)
		} else {
			s.quiet.failed(ctx, e)
		}
	}
}

func (s *server) needsYou(ctx context.Context, n notifyPayload) bool {
	row, ok, err := s.sessionRow(ctx, n.Workspace, n.Session)
	if err != nil {
		return true
	}
	return ok && row.NeedsYou
}

func (a *app) escalateLoop(ctx context.Context) {
	t := time.NewTicker(escalateEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.mu.Lock()
		enabled := a.enabled
		a.mu.Unlock()
		a.srv.escalate(ctx, enabled)
	}
}
