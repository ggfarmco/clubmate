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
			out = append(out, rpc.Command{ID: c.ID, Title: c.Title, Plugin: st.ID})
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
	var out json.RawMessage
	err := conn.Call(ctx, method, params, &out)
	if errors.Is(err, jsonrpc2.ErrClosed) {
		return nil, fmt.Errorf("%w: %s", ErrPluginUnavailable, pluginID)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin %s %s: %w", pluginID, method, err)
	}
	return out, nil
}
