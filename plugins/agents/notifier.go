package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	changeUpdate  = "update"
	changeStatus  = api.ChangeStatus
	changeCreated = api.ChangeCreated
	changeDeleted = api.ChangeDeleted
)

type notifier struct {
	host  *pluginsdk.Host
	mu    sync.Mutex
	order []change
	seen  map[change]bool
	saves map[*session]bool
	turns []api.Turn
	wake  chan struct{}
}

type change struct {
	session *session
	kind    string
}

func newNotifier(h *pluginsdk.Host) *notifier {
	n := &notifier{host: h, seen: map[change]bool{}, saves: map[*session]bool{}, wake: make(chan struct{}, 1)}
	go n.run()
	return n
}

func (n *notifier) mark(s *session, kind string, save bool) {
	publish := kind != changeUpdate
	if !publish && !save {
		return
	}
	n.mu.Lock()
	c := change{session: s, kind: kind}
	if publish && !n.seen[c] {
		n.seen[c] = true
		n.order = append(n.order, c)
	}
	if save {
		n.saves[s] = true
	}
	n.mu.Unlock()
	n.poke()
}

func (n *notifier) turn(p api.Turn) {
	n.mu.Lock()
	n.turns = append(n.turns, p)
	n.mu.Unlock()
	n.poke()
}

func (n *notifier) poke() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *notifier) run() {
	for range n.wake {
		n.mu.Lock()
		order, saves, turns := n.order, n.saves, n.turns
		n.order, n.seen, n.saves, n.turns = nil, map[change]bool{}, map[*session]bool{}, nil
		n.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		for s := range saves {
			n.persist(ctx, s)
		}
		for _, c := range order {
			rec := c.session.snapshot()
			if _, err := n.host.Publish(ctx, api.TopicChanged, api.Changed{Workspace: rec.Workspace, Session: rec.ID, Kind: c.kind}); err != nil {
				fmt.Fprintln(os.Stderr, "agents: publish:", err)
			}
		}
		for _, p := range turns {
			if _, err := n.host.Publish(ctx, api.TopicTurn, p); err != nil {
				fmt.Fprintln(os.Stderr, "agents: publish:", err)
			}
		}
		cancel()
	}
}

func (n *notifier) persist(ctx context.Context, s *session) {
	s.mu.Lock()
	rec, deleted := s.rec, s.deleted
	s.mu.Unlock()
	var err error
	if deleted {
		err = n.host.StorageDelete(ctx, sessionKey+rec.ID)
	} else {
		err = n.host.StorageSet(ctx, sessionKey+rec.ID, rec)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: save session:", err)
	}
}
