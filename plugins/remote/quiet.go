package main

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
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
}

type quietDecision struct {
	here bool
	at   time.Time
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

func (l *quietLog) decide(id string, here func() bool) bool {
	if id == "" {
		return here()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	maps.DeleteFunc(l.decided, func(_ string, d quietDecision) bool { return now.Sub(d.at) > pushMaxAge })
	if d, ok := l.decided[id]; ok {
		return d.here
	}
	v := here()
	l.decided[id] = quietDecision{here: v, at: now}
	return v
}

func (l *quietLog) quieted(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.decided[id].here
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
	if err := l.kv.StorageDelete(ctx, quietPrefix+id); err != nil {
		logf("drop quiet notification %s: %v", id, err)
	}
}

func (l *quietLog) resolved(ctx context.Context, ev pluginsdk.Event) {
	var c agentsChanged
	if err := json.Unmarshal(ev.Payload, &c); err != nil || c.Kind != changeResolved || c.Session == "" {
		return
	}
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

func (s *server) quietHere(n notifyPayload) bool {
	return !n.Quiet && s.quiet.decide(n.ID, s.presence.atMac)
}

func (s *server) quietEvent(ev pluginsdk.Event) pluginsdk.Event {
	if ev.Topic != notifyTopic {
		return ev
	}
	var n notifyPayload
	if !s.relay.configured() {
		return ev
	}
	if err := json.Unmarshal(ev.Payload, &n); err != nil || !s.quietHere(n) {
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
