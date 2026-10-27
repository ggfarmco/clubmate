package rpc

import (
	"context"
	"encoding/json"
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

func (p *presence) join(b *bus.Bus, projectID string) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	p.counts[projectID]++
	if p.counts[projectID] == 1 {
		publishPresence(b, topicUIOpened, projectID)
	}
	return func() { p.leave(b, projectID) }
}

func (p *presence) leave(b *bus.Bus, projectID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[projectID]--
	if p.counts[projectID] == 0 {
		delete(p.counts, projectID)
		publishPresence(b, topicUIIdle, projectID)
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

func publishPresence(b *bus.Bus, topic, projectID string) {
	if b == nil {
		return
	}
	raw, err := json.Marshal(projectParams{ProjectID: projectID})
	if err != nil {
		return
	}
	_, _ = b.Publish(context.Background(), topic, coreSource, raw)
}
