package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	changeUpdate   = "update"
	changeStatus   = api.ChangeStatus
	changeCreated  = api.ChangeCreated
	changeDeleted  = api.ChangeDeleted
	changeResolved = api.ChangeResolved
	changeReview   = api.ChangeReview

	notifyPermission = "permission"
	notifyQuestion   = "question"
	notifyAsks       = "needs-input"
	notifyDone       = "done"
	notifyStuck      = "stuck"

	endPostMax    = 2
	endPostWindow = 10 * time.Second
	endHold       = 5 * time.Second
	endHoldEnv    = "CLUBMATE_AGENTS_END_HOLD"

	doneOpenFormat   = "Done · %d open"
	listedOpenFormat = "%s · %d open"
)

var (
	permObjects = map[string]string{"execute": "a command", "edit": "a file", "delete": "a file", "move": "a file", "read": "a file", "fetch": "a URL"}
	endMessages = map[string]string{notifyAsks: "Asks you a question", notifyStuck: "Stopped with an error", notifyDone: "Finished"}
	endKinds    = []string{notifyAsks, notifyStuck, notifyDone}
)

type notifier struct {
	host  *pluginsdk.Host
	mu    sync.Mutex
	order []change
	seen  map[change]bool
	saves map[*session]bool
	turns []api.Turn
	asks  []change
	ends  []ending
	endq  endQueue
	timer *time.Timer
	wake  chan struct{}
}

type ending struct {
	workspace string
	session   string
	title     string
	kind      string
	policy    string
	open      int
	quiet     bool
	due       time.Time
}

type endQueue struct {
	hold time.Duration
	held map[string][]ending
	sent map[string][]time.Time
}

type notifyPost struct {
	WorkspaceID string `json:"workspaceId"`
	Session     string `json:"session"`
	Collapse    string `json:"collapse,omitempty"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message"`
	Kind        string `json:"kind"`
	Quiet       bool   `json:"quiet,omitempty"`
}

type change struct {
	session     *session
	kind        string
	request     string
	requestKind string
}

func newNotifier(h *pluginsdk.Host) *notifier {
	n := &notifier{host: h, seen: map[change]bool{}, saves: map[*session]bool{}, endq: newEndQueue(envDelay(endHoldEnv, endHold)), wake: make(chan struct{}, 1)}
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

func (n *notifier) ended(e ending) {
	n.mu.Lock()
	n.ends = append(n.ends, e)
	n.mu.Unlock()
	n.poke()
}

func (n *notifier) superseded(session string) {
	n.ended(ending{session: session})
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
		order, saves, turns, asks, ends := n.order, n.saves, n.turns, n.asks, n.ends
		n.order, n.seen, n.saves, n.turns, n.asks, n.ends = nil, map[change]bool{}, map[*session]bool{}, nil, nil, nil
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
		now := time.Now()
		for _, e := range ends {
			if e.kind == "" {
				n.endq.drop(e.session)
			} else {
				n.endq.add(e, now)
			}
		}
		posts, wait := n.endq.take(now)
		for _, p := range posts {
			n.post(ctx, p)
		}
		if n.timer != nil {
			n.timer.Stop()
		}
		if wait > 0 {
			n.timer = time.AfterFunc(wait, n.poke)
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
	kind, message := notifyPermission, "Wants permission"
	if verb, ok := permVerbs[summary.Tool]; ok {
		message = "Wants to " + verb + " " + permObjects[summary.Tool]
	}
	if c.requestKind == entryElicitation {
		kind, message = notifyQuestion, "Has a question"
	}
	n.post(ctx, notifyPost{WorkspaceID: rec.Workspace, Session: rec.ID, Collapse: rec.ID, Title: rec.Title, Message: message, Kind: kind})
}

func (n *notifier) post(ctx context.Context, req notifyPost) {
	if err := n.host.Call(ctx, "notify.post", req, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: notify:", err)
	}
}

func (a *app) notifyEnd(s *session, t api.Turn) {
	kind := notifyDone
	switch {
	case t.Status != statusError && t.Status != statusDone:
		return
	case s.snapshot().connectWaits() || s.snapshot().taskWaits():
		kind = notifyAsks
	case t.Status == statusError:
		kind = notifyStuck
	case t.Asks:
		kind = notifyAsks
	}
	rec := s.snapshot()
	if _, reminded := a.reminders.at(rec.Workspace, rec.ID); reminded && kind != notifyStuck {
		return
	}
	if rec.ParentSession != "" || rec.restricted() || rec.Run != nil && kind != notifyAsks || kind == notifyDone && a.runningChildren(rec.ID) > 0 {
		return
	}
	a.notes.ended(ending{workspace: rec.Workspace, session: rec.ID, title: rec.Title, kind: kind, policy: a.effectiveNotify(rec), open: rec.OpenCount, quiet: kind == notifyDone && (rec.OpenCount > 0 || s.askedInTurn())})
}

func (e ending) allowed() bool {
	return e.policy == notifyAll || e.policy == notifyNeedsInput && e.kind == notifyAsks
}

func (e ending) message() string {
	if e.kind == notifyDone && e.open > 0 {
		return fmt.Sprintf(doneOpenFormat, e.open)
	}
	return endMessages[e.kind]
}

func (e ending) listed() string {
	if e.kind == notifyDone && e.open > 0 {
		return fmt.Sprintf(listedOpenFormat, e.title, e.open)
	}
	return e.title
}

func newEndQueue(hold time.Duration) endQueue {
	return endQueue{hold: hold, held: map[string][]ending{}, sent: map[string][]time.Time{}}
}

func (q *endQueue) add(e ending, now time.Time) {
	e.due = now.Add(q.hold)
	held := slices.DeleteFunc(q.held[e.workspace], func(h ending) bool { return h.session == e.session })
	q.held[e.workspace] = append(held, e)
}

func (q *endQueue) drop(session string) {
	for ws, held := range q.held {
		held = slices.DeleteFunc(held, func(h ending) bool { return h.session == session })
		if len(held) == 0 {
			delete(q.held, ws)
		} else {
			q.held[ws] = held
		}
	}
}

func (q *endQueue) take(now time.Time) ([]notifyPost, time.Duration) {
	var posts []notifyPost
	var wait time.Duration
	sooner := func(d time.Duration) {
		if wait == 0 || d < wait {
			wait = d
		}
	}
	for ws, held := range q.held {
		var ripe, waiting []ending
		for _, e := range held {
			if e.due.After(now) {
				waiting = append(waiting, e)
				sooner(e.due.Sub(now))
			} else {
				ripe = append(ripe, e)
			}
		}
		if len(ripe) == 0 {
			continue
		}
		sent := slices.DeleteFunc(q.sent[ws], func(t time.Time) bool { return !t.After(now.Add(-endPostWindow)) })
		if len(sent) >= endPostMax {
			q.sent[ws] = sent
			sooner(sent[0].Add(endPostWindow).Sub(now))
			continue
		}
		if len(waiting) == 0 {
			delete(q.held, ws)
		} else {
			q.held[ws] = waiting
		}
		if post, ok := endPost(ripe); ok {
			posts = append(posts, post)
			q.sent[ws] = append(sent, now)
		}
	}
	return posts, wait
}

func endPost(held []ending) (notifyPost, bool) {
	if len(held) > 1 {
		held = slices.DeleteFunc(held, func(e ending) bool { return !e.allowed() })
	}
	switch len(held) {
	case 0:
		return notifyPost{}, false
	case 1:
		e := held[0]
		return notifyPost{WorkspaceID: e.workspace, Session: e.session, Collapse: e.session, Title: e.title, Message: e.message(), Kind: e.kind, Quiet: e.quiet}, true
	}
	post := notifyPost{WorkspaceID: held[0].workspace, Title: fmt.Sprintf("%d agent sessions", len(held))}
	var lines []string
	for _, kind := range endKinds {
		var titles []string
		for _, e := range held {
			if e.kind == kind {
				titles = append(titles, e.listed())
			}
		}
		if len(titles) > 0 {
			post.Kind = cmp.Or(post.Kind, kind)
			lines = append(lines, endMessages[kind]+": "+strings.Join(titles, ", "))
		}
	}
	post.Message = strings.Join(lines, "\n")
	return post, true
}

func (n *notifier) persist(ctx context.Context, s *session) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
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
