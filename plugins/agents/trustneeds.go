package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
)

const (
	kindGate         = "gate"
	trustRequest     = "trust:"
	trustPollEnv     = "CLUBMATE_AGENTS_TRUST_POLL"
	trustPollDefault = 30 * time.Second
)

type trustNeed struct {
	Gate  string
	Name  string
	Point string
	Waits int
	Since time.Time
}

type trustNeeds struct {
	mu     sync.Mutex
	syncMu sync.Mutex
	byWS   map[string]map[string]bool
}

func holdWaitsTrust(h *heldTurn) bool {
	return h != nil && h.Wait == graphs.ErrGateGraphChanged.Error() && !h.Unread
}

func trustHolds(rec record) []*heldTurn {
	if rec.closed() {
		return nil
	}
	var out []*heldTurn
	if holdWaitsTrust(rec.Held) {
		out = append(out, rec.Held)
	}
	for _, c := range rec.Calls {
		if holdWaitsTrust(c.Hold) {
			out = append(out, c.Hold)
		}
	}
	return out
}

func waitsForTrust(rec record) []string {
	var out []string
	for _, h := range trustHolds(rec) {
		if !slices.Contains(out, h.Gate) {
			out = append(out, h.Gate)
		}
	}
	slices.Sort(out)
	return out
}

func (s *session) trustFlipped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	gates := strings.Join(waitsForTrust(s.rec), ",")
	was := s.trustOf
	s.trustOf = gates
	return gates != was
}

func (n *trustNeeds) seed(rec record) {
	gates := waitsForTrust(rec)
	if len(gates) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.byWS == nil {
		n.byWS = map[string]map[string]bool{}
	}
	if n.byWS[rec.Workspace] == nil {
		n.byWS[rec.Workspace] = map[string]bool{}
	}
	for _, gate := range gates {
		n.byWS[rec.Workspace][gate] = true
	}
}

func (n *trustNeeds) waiting(workspace, gate string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.byWS[workspace][gate]
}

func (n *trustNeeds) swap(workspace string, now map[string]bool) map[string]bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.byWS == nil {
		n.byWS = map[string]map[string]bool{}
	}
	was := n.byWS[workspace]
	n.byWS[workspace] = now
	return was
}

func (a *app) trustWaiting(workspace string) []trustNeed {
	return trustNeedsOf(a.openRecordsIn(workspace))
}

func trustNeedsOf(recs []record) []trustNeed {
	byGate := map[string]*trustNeed{}
	for _, rec := range recs {
		for _, h := range trustHolds(rec) {
			since := cmp.Or(h.Since, rec.UpdatedAt)
			n := byGate[h.Gate]
			if n == nil {
				n = &trustNeed{Gate: h.Gate, Since: since}
				byGate[h.Gate] = n
			}
			n.Waits++
			if since.Before(n.Since) {
				n.Since = since
			}
		}
	}
	var out []trustNeed
	for _, gate := range slices.Sorted(maps.Keys(byGate)) {
		out = append(out, *byGate[gate])
	}
	return out
}

func (a *app) trustGate(gate string) (name, point string) {
	ctx := context.Background()
	if g := a.gateByID(ctx, gate); g != nil {
		return a.gateMatchName(ctx, *g), g.Point
	}
	return gate, ""
}

func (a *app) trustSynced(workspace string) {
	a.trustNeeds.syncMu.Lock()
	defer a.trustNeeds.syncMu.Unlock()
	needs := a.trustWaiting(workspace)
	now := map[string]bool{}
	for _, n := range needs {
		now[n.Gate] = true
	}
	was := a.trustNeeds.swap(workspace, now)
	for _, n := range needs {
		if !was[n.Gate] {
			go func(n trustNeed) {
				title, _ := a.trustGate(n.Gate)
				still := func() bool { return a.trustNeeds.waiting(workspace, n.Gate) }
				a.notes.cardedIf(notifyPost{WorkspaceID: workspace, Session: n.Gate, Collapse: n.Gate, Title: title, Message: trustWords(n.Waits), Kind: notifyAsks}, still)
			}(n)
		}
	}
	if a.host == nil {
		return
	}
	for _, gate := range slices.Sorted(maps.Keys(was)) {
		if now[gate] {
			continue
		}
		c := api.Changed{Workspace: workspace, Session: gate, Kind: api.ChangeResolved, Request: trustRequest + gate, RequestKind: api.RequestGate}
		a.notes.withdrawn(c)
	}
}

func trustWords(waits int) string {
	return fmt.Sprintf(gateTrustWaits, gateTrustRow, waits)
}

func trustInfo(workspace string, n trustNeed) sessionInfo {
	since := n.Since
	return sessionInfo{
		record:          record{ID: n.Gate, Workspace: workspace, Title: cmp.Or(n.Name, n.Gate), Status: statusWaiting, CreatedAt: since, UpdatedAt: since},
		Kind:            kindGate,
		RowDetail:       trustWords(n.Waits),
		NeedsYou:        true,
		NeedsYouSince:   &since,
		Group:           groupNeedsYou,
		NotifyEffective: notifyAll,
		gatePoint:       n.Point,
	}
}

func needEntries(needs map[string]runNeed, infos []sessionInfo) []string {
	out := slices.Sorted(maps.Keys(needs))
	for _, info := range infos {
		if info.Kind == kindGate {
			out = append(out, info.ID)
		}
	}
	return out
}

func (a *app) pollTrust(r holdRef) {
	id := r.s.snapshot().ID + "/" + r.gate
	if a.trustPoll <= 0 {
		return
	}
	if _, busy := a.trustPolls.LoadOrStore(id, true); busy {
		return
	}
	time.AfterFunc(a.trustPoll, func() {
		a.trustPolls.Delete(id)
		if h := r.get(); h != nil && h.Wait != "" && !h.Unread {
			a.reviewHeld(r)
		}
	})
}
