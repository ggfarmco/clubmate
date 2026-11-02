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
	changeUpdate   = "update"
	changeStatus   = api.ChangeStatus
	changeCreated  = api.ChangeCreated
	changeDeleted  = api.ChangeDeleted
	changeResolved = api.ChangeResolved

	notifyPermission = "permission"
	notifyQuestion   = "question"
)

type notifier struct {
	host  *pluginsdk.Host
	mu    sync.Mutex
	order []change
	seen  map[change]bool
	saves map[*session]bool
	turns []api.Turn
	asks  []change
	wake  chan struct{}
}

type notifyPost struct {
	WorkspaceID string `json:"workspaceId"`
	Session     string `json:"session"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message"`
	Kind        string `json:"kind"`
}

type change struct {
	session     *session
	kind        string
	request     string
	requestKind string
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

func (n *notifier) resolved(s *session, kind, request string) {
	n.mu.Lock()
	n.order = append(n.order, change{session: s, kind: changeResolved, request: request, requestKind: kind})
	n.mu.Unlock()
	n.poke()
}

func (n *notifier) requested(s *session, kind, request string) {
	n.mu.Lock()
	n.asks = append(n.asks, change{session: s, request: request, requestKind: kind})
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
		order, saves, turns, asks := n.order, n.saves, n.turns, n.asks
		n.order, n.seen, n.saves, n.turns, n.asks = nil, map[change]bool{}, map[*session]bool{}, nil, nil
		n.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		for s := range saves {
			n.persist(ctx, s)
		}
		for _, c := range order {
			rec := c.session.snapshot()
			if _, err := n.host.Publish(ctx, api.TopicChanged, api.Changed{Workspace: rec.Workspace, Session: rec.ID, Kind: c.kind, Status: rec.Status, Request: c.request, RequestKind: c.requestKind}); err != nil {
				fmt.Fprintln(os.Stderr, "agents: publish:", err)
			}
		}
		for _, p := range turns {
			if _, err := n.host.Publish(ctx, api.TopicTurn, p); err != nil {
				fmt.Fprintln(os.Stderr, "agents: publish:", err)
			}
		}
		for _, c := range asks {
			n.notify(ctx, c)
		}
		cancel()
	}
}

func (n *notifier) notify(ctx context.Context, c change) {
	summary := c.session.requestSummary(c.requestKind, c.request)
	if summary == nil {
		return
	}
	rec := c.session.snapshot()
	kind, message := notifyPermission, "Permission needed"
	if c.requestKind == entryElicitation {
		kind, message = notifyQuestion, "Question"
	}
	if summary.Text != "" {
		message = summary.Text
	}
	req := notifyPost{WorkspaceID: rec.Workspace, Session: rec.ID, Title: rec.Title, Message: message, Kind: kind}
	if err := n.host.Call(ctx, "notify.post", req, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: notify:", err)
	}
}

func (n *notifier) persist(ctx context.Context, s *session) {
	s.mu.Lock()
	rec, deleted := s.rec, s.deleted
	s.mu.Unlock()
	var err error
	if deleted {
		err = n.host.StorageDelete(ctx, sessionStorageKey(rec))
	} else {
		err = n.host.StorageSet(ctx, sessionStorageKey(rec), rec)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: save session:", err)
	}
}
