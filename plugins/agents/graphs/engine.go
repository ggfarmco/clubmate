package graphs

import (
	"context"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

type Deps struct {
	Host       *pluginsdk.Host
	StateDir   string
	Sessions   func(ctx context.Context, workspace string) ([]api.Session, error)
	Needs      func(ctx context.Context, workspace string, needs []api.RunNeed)
	ShowRun    func(ctx context.Context, workspace, run string) error
	Changed    func(workspace string)
	PruneAfter time.Duration
}

type Engine struct {
	a          *app
	subscribed chan struct{}
	settle     sync.Once
	mu         sync.Mutex
	queue      []func()
	wake       chan struct{}
}

func New(d Deps) *Engine {
	a := newApp()
	a.host, a.stateDir, a.sessions, a.needs, a.pruneAfter = d.Host, d.StateDir, d.Sessions, d.Needs, d.PruneAfter
	if d.ShowRun != nil {
		a.showRun = d.ShowRun
	}
	if d.Changed != nil {
		a.changed = d.Changed
	}
	e := &Engine{a: a, subscribed: make(chan struct{}), wake: make(chan struct{}, 1)}
	a.onSubscribed = e.markSubscribed
	return e
}

func (e *Engine) Start(ctx context.Context, after int64) error {
	go e.drain(ctx)
	err := e.a.startEngine(ctx, after)
	if err != nil {
		e.a.mu.Lock()
		e.a.startErr = err
		e.a.mu.Unlock()
		e.markSubscribed()
		close(e.a.ready)
	}
	return err
}

func (e *Engine) markSubscribed() {
	e.settle.Do(func() { close(e.subscribed) })
}

func (e *Engine) Owns(sub string) bool {
	if e.owns(sub) {
		return true
	}
	<-e.subscribed
	return e.owns(sub)
}

func (e *Engine) owns(sub string) bool {
	e.a.mu.Lock()
	defer e.a.mu.Unlock()
	_, ok := e.a.subs[sub]
	return ok
}

func (e *Engine) BusEvent(ctx context.Context, sub string, ev pluginsdk.Event) {
	e.mu.Lock()
	e.queue = append(e.queue, func() { e.a.busEvent(ctx, nil, sub, ev) })
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) drain(ctx context.Context) {
	for {
		e.mu.Lock()
		var fn func()
		if len(e.queue) > 0 {
			fn, e.queue = e.queue[0], e.queue[1:]
		}
		e.mu.Unlock()
		if fn != nil {
			fn()
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		}
	}
}

func (e *Engine) BusClosed(ctx context.Context, sub string, err error) {
	e.a.busClosed(ctx, nil, sub, err)
}

func (e *Engine) Command(ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
	return e.a.command(ctx, nil, cmd)
}

func (e *Engine) UIEvent(ctx context.Context, ev pluginsdk.UIEvent) (any, error) {
	return e.a.uiEvent(ctx, nil, ev)
}

func (e *Engine) Running(workspace string) map[string]bool {
	select {
	case <-e.a.ready:
	default:
		return nil
	}
	e.a.mu.Lock()
	failed := e.a.startErr != nil
	e.a.mu.Unlock()
	if failed {
		return nil
	}
	running := map[string]bool{}
	for _, r := range e.a.workspaceRuns(workspace, true) {
		if r.Status != statusStopped {
			running[r.ID] = true
		}
	}
	return running
}

type pageKey struct{}

func fromPage(ctx context.Context) context.Context {
	return context.WithValue(ctx, pageKey{}, true)
}

func onPage(ctx context.Context) bool {
	return ctx.Value(pageKey{}) != nil
}

func (e *Engine) PageEvent(ctx context.Context, ev pluginsdk.UIEvent) (any, error) {
	return e.a.uiEvent(fromPage(ctx), nil, ev)
}

func (e *Engine) Refused(workspace string) int {
	e.a.mu.Lock()
	defer e.a.mu.Unlock()
	n := 0
	for key := range e.a.refused {
		if strings.HasPrefix(key, workspace+"\n") {
			n++
		}
	}
	return n
}

func (e *Engine) Page(ctx context.Context, workspace, id string) sdui.Node {
	return e.a.page(ctx, workspace, id)
}

func (a *app) waitReady(ctx context.Context) error {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startErr
}
