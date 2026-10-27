package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/rpc"
)

const routeTimeout = 5 * time.Second

var ErrPluginUnavailable = rpc.ErrPluginUnavailable

type Router struct {
	sup *Supervisor
}

func NewRouter(sup *Supervisor) *Router {
	return &Router{sup: sup}
}

func (r *Router) UIEvent(ctx context.Context, pluginID string, ev json.RawMessage) (json.RawMessage, error) {
	return r.call(ctx, pluginID, "ui.event", ev)
}

func (r *Router) Command(ctx context.Context, pluginID string, run json.RawMessage) (json.RawMessage, error) {
	return r.call(ctx, pluginID, "command.run", run)
}

func (r *Router) Commands() []rpc.Command {
	var out []rpc.Command
	for _, st := range r.sup.Statuses() {
		for _, c := range st.Manifest.Contributes.Commands {
			out = append(out, rpc.Command{ID: c.ID, Title: c.Title, Plugin: st.ID, Hidden: c.Hidden, Confirm: c.Confirm})
		}
	}
	return out
}

func (r *Router) call(ctx context.Context, pluginID, method string, params json.RawMessage) (json.RawMessage, error) {
	conn, ok := r.sup.Conn(pluginID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPluginUnavailable, pluginID)
	}
	ctx, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	return callConn(ctx, conn, pluginID, method, params)
}

func callConn(ctx context.Context, conn *jsonrpc2.Conn, pluginID, method string, params json.RawMessage) (json.RawMessage, error) {
	type reply struct {
		out json.RawMessage
		err error
	}
	replies := make(chan reply, 1)
	go func() {
		var out json.RawMessage
		err := conn.Call(ctx, method, params, &out)
		replies <- reply{out: out, err: err}
	}()
	select {
	case r := <-replies:
		if r.err != nil {
			return nil, callError(pluginID, method, r.err)
		}
		return r.out, nil
	case <-ctx.Done():
		return nil, callError(pluginID, method, ctx.Err())
	}
}

func callError(pluginID, method string, err error) error {
	var rpcErr *jsonrpc2.Error
	switch {
	case errors.As(err, &rpcErr):
		return &rpc.PluginError{PluginID: pluginID, Method: method, Message: rpcErr.Message}
	case errors.Is(err, jsonrpc2.ErrClosed), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: plugin %s %s: %w", ErrPluginUnavailable, pluginID, method, err)
	}
	return fmt.Errorf("plugin %s %s: %w", pluginID, method, err)
}
