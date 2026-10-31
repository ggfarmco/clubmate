package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
	"clubmate/pkg/contract"
)

const coreSource = "core"

type presence struct {
	mu      sync.Mutex
	counts  map[string]int
	targets map[string]map[*focusTarget]struct{}
}

type focusTarget struct {
	conn *jsonrpc2.Conn
	sid  string
	gone bool
}

func (p *presence) addTarget(workspaceID string, t *focusTarget, sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.gone {
		return
	}
	t.sid = sid
	if p.targets == nil {
		p.targets = map[string]map[*focusTarget]struct{}{}
	}
	if p.targets[workspaceID] == nil {
		p.targets[workspaceID] = map[*focusTarget]struct{}{}
	}
	p.targets[workspaceID][t] = struct{}{}
}

func (p *presence) dropTarget(workspaceID string, t *focusTarget) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t.gone = true
	delete(p.targets[workspaceID], t)
	if len(p.targets[workspaceID]) == 0 {
		delete(p.targets, workspaceID)
	}
}

func (p *presence) focusTargets(workspaceID string) []focusTarget {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]focusTarget, 0, len(p.targets[workspaceID]))
	for t := range p.targets[workspaceID] {
		out = append(out, focusTarget{conn: t.conn, sid: t.sid})
	}
	return out
}

func (p *presence) join(b *bus.Bus, logger *slog.Logger, workspaceID string) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	p.counts[workspaceID]++
	if p.counts[workspaceID] == 1 {
		publishPresence(b, logger, contract.TopicUIOpened, workspaceID)
	}
	return func() { p.leave(b, logger, workspaceID) }
}

func (p *presence) leave(b *bus.Bus, logger *slog.Logger, workspaceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[workspaceID]--
	if p.counts[workspaceID] == 0 {
		delete(p.counts, workspaceID)
		publishPresence(b, logger, contract.TopicUIIdle, workspaceID)
	}
}

func (p *presence) workspaces() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.counts))
	for id := range p.counts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func publishPresence(b *bus.Bus, logger *slog.Logger, topic, workspaceID string) {
	if b == nil {
		return
	}
	raw, err := json.Marshal(contract.UIPresence{Workspace: workspaceID})
	if err != nil {
		logger.Error("marshal presence event", "topic", topic, "err", err)
		return
	}
	if _, err := b.Publish(context.Background(), topic, coreSource, raw); err != nil {
		logger.Error("publish presence event", "topic", topic, "err", err)
	}
}
