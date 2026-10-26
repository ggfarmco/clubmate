package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
)

const (
	notifyBusEvent  = "bus.event"
	notifyBusClosed = "bus.closed"
)

var (
	errNoBus         = errors.New("event bus not configured")
	errEmptyConsumer = errors.New("consumer is required")
)

type publishParams struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type readParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
	Limit   int    `json:"limit"`
}

type subscribeParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
}

type subscriptionParams struct {
	Subscription string `json:"subscription"`
}

type cursorParams struct {
	Consumer string `json:"consumer"`
	Seq      int64  `json:"seq"`
}

type seqResult struct {
	Seq int64 `json:"seq"`
}

type busEventParams struct {
	Subscription string    `json:"subscription"`
	Event        bus.Event `json:"event"`
}

type closedParams struct {
	Subscription string `json:"subscription"`
	Error        string `json:"error"`
}

func (s *Server) busMethods() map[string]handlerFunc {
	b := s.bus
	methods := map[string]handlerFunc{
		"bus.publish": method(func(ctx context.Context, p publishParams) (bus.Event, error) {
			return b.Publish(ctx, p.Topic, CallerFrom(ctx).String(), p.Payload)
		}),
		"bus.read": method(func(ctx context.Context, p readParams) ([]bus.Event, error) {
			events, err := b.Read(ctx, p.Pattern, p.After, p.Limit)
			return nonNil(events), err
		}),
		"bus.subscribe": method(func(ctx context.Context, p subscribeParams) (subscriptionParams, error) {
			sub, err := b.Subscribe(p.Pattern, p.After)
			if err != nil {
				return subscriptionParams{}, err
			}
			sid, err := subscriptionsFrom(ctx).add(kindBus, sub.Close, forwardBus(ctx, connFrom(ctx), sub))
			return subscriptionParams{Subscription: sid}, err
		}),
		"bus.unsubscribe": method(func(ctx context.Context, p subscriptionParams) (empty, error) {
			return empty{}, subscriptionsFrom(ctx).cancel(kindBus, p.Subscription)
		}),
		"bus.cursor.get": method(func(ctx context.Context, p cursorParams) (seqResult, error) {
			if p.Consumer == "" {
				return seqResult{}, errEmptyConsumer
			}
			seq, err := b.Cursor(ctx, p.Consumer)
			return seqResult{Seq: seq}, err
		}),
		"bus.cursor.set": method(func(ctx context.Context, p cursorParams) (empty, error) {
			if p.Consumer == "" {
				return empty{}, errEmptyConsumer
			}
			return empty{}, b.SetCursor(ctx, p.Consumer, p.Seq)
		}),
	}
	if b == nil {
		for name := range methods {
			methods[name] = busUnavailable
		}
	}
	return methods
}

func busUnavailable(context.Context, *json.RawMessage) (any, error) {
	return nil, errNoBus
}

func forwardBus(ctx context.Context, conn *jsonrpc2.Conn, sub *bus.Subscription) forwardFunc {
	return func(sid string, done <-chan struct{}) {
		for {
			select {
			case <-done:
				return
			case e, ok := <-sub.C():
				if !ok {
					notifyDropped(ctx, conn, notifyBusClosed, sid, sub.Err())
					return
				}
				if err := conn.Notify(ctx, notifyBusEvent, busEventParams{Subscription: sid, Event: e}); err != nil {
					return
				}
			}
		}
	}
}

func notifyDropped(ctx context.Context, conn *jsonrpc2.Conn, method, sid string, err error) {
	if err == nil {
		return
	}
	_ = conn.Notify(ctx, method, closedParams{Subscription: sid, Error: err.Error()})
}
