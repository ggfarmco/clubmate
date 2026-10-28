package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
)

const (
	topicUIOpened = "ui.opened"
	topicUIIdle   = "ui.idle"
	coreSource    = "core"
)

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

func (p *presence) addTarget(projectID string, t *focusTarget, sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.gone {
		return
	}
	t.sid = sid
	if p.targets == nil {
		p.targets = map[string]map[*focusTarget]struct{}{}
	}
	if p.targets[projectID] == nil {
		p.targets[projectID] = map[*focusTarget]struct{}{}
	}
	p.targets[projectID][t] = struct{}{}
}

func (p *presence) dropTarget(projectID string, t *focusTarget) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t.gone = true
	delete(p.targets[projectID], t)
	if len(p.targets[projectID]) == 0 {
		delete(p.targets, projectID)
	}
}

func (p *presence) focusTargets(projectID string) []focusTarget {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]focusTarget, 0, len(p.targets[projectID]))
	for t := range p.targets[projectID] {
		out = append(out, focusTarget{conn: t.conn, sid: t.sid})
	}
	return out
}

func (p *presence) join(b *bus.Bus, logger *slog.Logger, projectID string) (func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	p.counts[projectID]++
	opened := p.counts[projectID] == 1
	if opened {
		publishPresence(b, logger, topicUIOpened, projectID)
	}
	return func() { p.leave(b, logger, projectID) }, opened
}

func (p *presence) leave(b *bus.Bus, logger *slog.Logger, projectID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[projectID]--
	if p.counts[projectID] == 0 {
		delete(p.counts, projectID)
		publishPresence(b, logger, topicUIIdle, projectID)
	}
}

func (p *presence) projects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.counts))
	for id := range p.counts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func publishPresence(b *bus.Bus, logger *slog.Logger, topic, projectID string) {
	if b == nil {
		return
	}
	raw, err := json.Marshal(projectParams{ProjectID: projectID})
	if err != nil {
		logger.Error("marshal presence event", "topic", topic, "err", err)
		return
	}
	if _, err := b.Publish(context.Background(), topic, coreSource, raw); err != nil {
		logger.Error("publish presence event", "topic", topic, "err", err)
	}
}
