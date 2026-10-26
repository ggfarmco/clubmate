package rpc

import (
	"context"
	"errors"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/id"
)

const (
	subscriptionPrefix = "sub"

	kindBus = "bus"
	kindUI  = "ui"
)

var (
	errUnknownSubscription = errors.New("unknown subscription")
	errConnClosed          = errors.New("connection closed")
)

type forwardFunc func(subscription string, done <-chan struct{})

type subscriptions struct {
	mu      sync.Mutex
	closed  bool
	entries map[string]*subscriptionEntry
	wg      sync.WaitGroup
}

type subscriptionEntry struct {
	kind     string
	stop     func()
	done     chan struct{}
	finished chan struct{}
}

type subscriptionsKey struct{}

type connKey struct{}

func newSubscriptions() *subscriptions {
	return &subscriptions{entries: map[string]*subscriptionEntry{}}
}

func (r *subscriptions) add(kind string, stop func(), forward forwardFunc) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		stop()
		return "", errConnClosed
	}
	sid := id.New(subscriptionPrefix)
	e := &subscriptionEntry{kind: kind, stop: stop, done: make(chan struct{}), finished: make(chan struct{})}
	r.entries[sid] = e
	r.wg.Go(func() {
		defer r.remove(sid)
		defer close(e.finished)
		defer e.stop()
		forward(sid, e.done)
	})
	return sid, nil
}

func (r *subscriptions) cancel(kind, sid string) error {
	r.mu.Lock()
	e, ok := r.entries[sid]
	if !ok || e.kind != kind {
		r.mu.Unlock()
		return errUnknownSubscription
	}
	delete(r.entries, sid)
	r.mu.Unlock()
	e.end()
	return nil
}

func (r *subscriptions) closeAll() {
	r.mu.Lock()
	r.closed = true
	entries := r.entries
	r.entries = nil
	r.mu.Unlock()
	for _, e := range entries {
		e.end()
	}
	r.wg.Wait()
}

func (r *subscriptions) remove(sid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, sid)
}

func (e *subscriptionEntry) end() {
	close(e.done)
	e.stop()
	<-e.finished
}

func withSubscriptions(ctx context.Context, r *subscriptions) context.Context {
	return context.WithValue(ctx, subscriptionsKey{}, r)
}

func subscriptionsFrom(ctx context.Context) *subscriptions {
	r, _ := ctx.Value(subscriptionsKey{}).(*subscriptions)
	return r
}

func withConn(ctx context.Context, conn *jsonrpc2.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, conn)
}

func connFrom(ctx context.Context) *jsonrpc2.Conn {
	conn, _ := ctx.Value(connKey{}).(*jsonrpc2.Conn)
	return conn
}
