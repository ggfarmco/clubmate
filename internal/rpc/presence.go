package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"

	"clubmate/internal/bus"
)

const (
	topicUIOpened = "ui.opened"
	topicUIIdle   = "ui.idle"
	coreSource    = "core"
)

type presence struct {
	mu     sync.Mutex
	counts map[string]int
}

func (p *presence) join(b *bus.Bus, logger *slog.Logger, projectID string) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	p.counts[projectID]++
	if p.counts[projectID] == 1 {
		publishPresence(b, logger, topicUIOpened, projectID)
	}
	return func() { p.leave(b, logger, projectID) }
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
