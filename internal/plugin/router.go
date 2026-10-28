package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/rpc"
)

const routeTimeout = 5 * time.Second

var ErrPluginUnavailable = rpc.ErrPluginUnavailable

type Router struct {
	sup *Supervisor

	mu     sync.Mutex
	warned map[string]bool
}

func NewRouter(sup *Supervisor) *Router {
	return &Router{sup: sup, warned: map[string]bool{}}
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
			out = append(out, command(st.ID, c))
		}
	}
	return out
}

func (r *Router) Resolve(id string) (rpc.Command, bool) {
	for _, c := range r.Commands() {
		if c.ID == id {
			return c, true
		}
	}
	c, ok := r.capabilities()[id]
	return c, ok
}

func (r *Router) Capabilities() map[string]string {
	out := map[string]string{}
	for name, c := range r.capabilities() {
		out[name] = c.ID
	}
	return out
}

func (r *Router) capabilities() map[string]rpc.Command {
	type provider struct {
		cmd     rpc.Command
		running bool
	}
	providers := map[string][]provider{}
	for _, st := range r.sup.loaded() {
		for _, c := range st.Manifest.Contributes.Commands {
			for _, name := range c.Provides {
				providers[name] = append(providers[name], provider{cmd: command(st.ID, c), running: st.State == StateRunning})
			}
		}
	}
	out := make(map[string]rpc.Command, len(providers))
	for name, ps := range providers {
		chosen := ps[0]
		for _, p := range ps {
			if p.running {
				chosen = p
				break
			}
		}
		out[name] = chosen.cmd
		if len(ps) > 1 {
			ids := make([]string, 0, len(ps))
			for _, p := range ps {
				ids = append(ids, p.cmd.ID)
			}
			r.warnOnce(fmt.Sprintf("capability %s provided by %s; using %s", name, strings.Join(ids, " and "), chosen.cmd.ID))
		}
	}
	return out
}

func (r *Router) warnOnce(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.warned[msg] {
		return
	}
	r.warned[msg] = true
	r.sup.logger.Warn(msg)
}

func command(pluginID string, c CommandDef) rpc.Command {
	return rpc.Command{
		ID:          c.ID,
		Title:       c.Title,
		Description: c.Description,
		Plugin:      pluginID,
		Hidden:      c.Hidden,
		Confirm:     c.Confirm,
		Args:        c.Args,
		Result:      c.Result,
		Agent:       c.Agent,
		Destructive: c.Destructive,
		Provides:    c.Provides,
	}
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
