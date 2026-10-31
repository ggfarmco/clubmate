package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/rpc"
	"clubmate/pkg/pprofdump"
)

const routeTimeout = 5 * time.Second

var (
	ErrPluginUnavailable = rpc.ErrPluginUnavailable
	ErrUnknownPlugin     = rpc.ErrUnknownPlugin
)

type Router struct {
	sup *Supervisor

	mu     sync.Mutex
	warned map[string]bool

	tableMu sync.Mutex
	table   *routeTable
}

type routeTable struct {
	gen          uint64
	commands     []rpc.Command
	byID         map[string]rpc.Command
	capabilities map[string]rpc.Command
}

func NewRouter(sup *Supervisor) *Router {
	return &Router{sup: sup, warned: map[string]bool{}}
}

func (r *Router) UIEvent(ctx context.Context, pluginID string, ev json.RawMessage) (json.RawMessage, error) {
	return r.call(ctx, pluginID, "ui.event", ev, routeTimeout)
}

func (r *Router) Command(ctx context.Context, c rpc.Command, run json.RawMessage) (json.RawMessage, error) {
	timeout := routeTimeout
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}
	return r.call(ctx, c.Plugin, "command.run", run, timeout)
}

func (r *Router) Pprof(ctx context.Context, pluginID string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	return r.call(ctx, pluginID, pprofdump.Method, params, timeout)
}

func (r *Router) Commands() []rpc.Command {
	return slices.Clip(r.routes().commands)
}

func (r *Router) Resolve(id string) (rpc.Command, bool) {
	t := r.routes()
	if c, ok := t.byID[id]; ok {
		return c, true
	}
	c, ok := t.capabilities[id]
	return c, ok
}

func (r *Router) Capabilities() map[string]string {
	out := map[string]string{}
	for name := range r.routes().capabilities {
		if c, ok := r.Resolve(name); ok {
			out[name] = c.ID
		}
	}
	return out
}

func (r *Router) routes() *routeTable {
	gen := r.sup.generation()
	r.tableMu.Lock()
	defer r.tableMu.Unlock()
	if r.table != nil && r.table.gen == gen {
		return r.table
	}
	t := &routeTable{gen: gen, byID: map[string]rpc.Command{}, capabilities: r.capabilities()}
	for _, st := range r.sup.Statuses() {
		for _, c := range st.Manifest.Contributes.Commands {
			cmd := command(st, c)
			t.commands = append(t.commands, cmd)
			if _, dup := t.byID[cmd.ID]; !dup {
				t.byID[cmd.ID] = cmd
			}
		}
	}
	r.table = t
	return t
}

func (r *Router) capabilities() map[string]rpc.Command {
	providers := map[string][]rpc.Command{}
	for _, st := range r.sup.loaded() {
		for _, c := range st.Manifest.Contributes.Commands {
			for _, name := range c.Provides {
				providers[name] = append(providers[name], command(st, c))
			}
		}
	}
	out := make(map[string]rpc.Command, len(providers))
	for name, cs := range providers {
		chosen := cs[0]
		for _, c := range cs {
			if c.Running {
				chosen = c
				break
			}
		}
		out[name] = chosen
		if len(cs) > 1 {
			ids := make([]string, 0, len(cs))
			for _, c := range cs {
				ids = append(ids, c.ID)
			}
			r.warnOnce(fmt.Sprintf("capability %s provided by %s; using %s", name, strings.Join(ids, " and "), chosen.ID))
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

func command(st Status, c rpc.CommandDef) rpc.Command {
	return rpc.Command{CommandDef: c, Plugin: st.ID, Running: st.State == StateRunning}
}

func (r *Router) call(ctx context.Context, pluginID, method string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	conn, ok := r.sup.Conn(pluginID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPluginUnavailable, pluginID)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
