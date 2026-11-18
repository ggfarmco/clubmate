package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
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
	Waits int
	Since time.Time
}

type trustNeeds struct {
	mu     sync.Mutex
	syncMu sync.Mutex
	byWS   map[string]map[string]bool
}

func waitsForTrust(rec record) string {
	if h := rec.Held; h != nil && h.Wait == graphs.ErrGateGraphChanged.Error() && !h.Unread && !rec.closed() {
		return h.Gate
	}
	return ""
}

func (n *trustNeeds) seed(rec record) {
	gate := waitsForTrust(rec)
	if gate == "" {
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
	n.byWS[rec.Workspace][gate] = true
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
	byGate := map[string]*trustNeed{}
	for _, s := range a.workspaceSessions(workspace) {
		rec := s.snapshot()
		gate := waitsForTrust(rec)
		if gate == "" {
			continue
		}
		since := cmp.Or(rec.Held.Since, rec.UpdatedAt)
		n := byGate[gate]
		if n == nil {
			n = &trustNeed{Gate: gate, Since: since}
			byGate[gate] = n
		}
		n.Waits++
		if since.Before(n.Since) {
			n.Since = since
		}
	}
	var out []trustNeed
	for _, gate := range slices.Sorted(maps.Keys(byGate)) {
		out = append(out, *byGate[gate])
	}
	return out
}

func (a *app) trustGateName(gate string) string {
	ctx := context.Background()
	if g := a.gateByID(ctx, gate); g != nil {
		return a.gateMatchName(ctx, *g)
	}
	return gate
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
				title := a.trustGateName(n.Gate)
				if !a.trustNeeds.waiting(workspace, n.Gate) {
					return
				}
				a.notes.carded(notifyPost{WorkspaceID: workspace, Session: n.Gate, Collapse: n.Gate, Title: title, Message: trustWords(n.Waits), Kind: notifyAsks})
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
		if _, err := a.host.Publish(context.Background(), api.TopicChanged, c); err != nil {
			fmt.Fprintln(os.Stderr, "agents: publish:", err)
		}
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
	}
}

func gateEntries(infos []sessionInfo) []string {
	var out []string
	for _, info := range infos {
		if info.Kind == kindGate {
			out = append(out, info.ID)
		}
	}
	return out
}

func (a *app) pollTrust(s *session) {
	id := s.snapshot().ID
	if a.trustPoll <= 0 {
		return
	}
	if _, busy := a.trustPolls.LoadOrStore(id, true); busy {
		return
	}
	time.AfterFunc(a.trustPoll, func() {
		a.trustPolls.Delete(id)
		if h := s.snapshot().Held; h != nil && h.Wait != "" && !h.Unread {
			a.reviewHeld(s)
		}
	})
}
