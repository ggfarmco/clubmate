package plugin

import (
	"cmp"
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
	"clubmate/pkg/contract"
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

	tableMu  sync.Mutex
	table    *routeTable
	defaults map[string]string
}

type routeTable struct {
	gen          uint64
	commands     []rpc.Command
	byID         map[string]rpc.Command
	capabilities map[string]rpc.Command
}

func NewRouter(sup *Supervisor) *Router {
	return &Router{sup: sup, warned: map[string]bool{}, defaults: map[string]string{}}
}

func (r *Router) SetDefault(capability, pluginID string) {
	r.tableMu.Lock()
	defer r.tableMu.Unlock()
	if pluginID == "" {
		delete(r.defaults, capability)
	} else {
		r.defaults[capability] = pluginID
	}
	r.table = nil
}

func (r *Router) Providers(capability string) rpc.CapabilityProviders {
	out := rpc.CapabilityProviders{Providers: []rpc.CapabilityProvider{}}
	if c, ok := r.routes().capabilities[capability]; ok {
		out.Default = c.Plugin
	}
	for _, st := range r.sup.Statuses() {
		if slices.ContainsFunc(st.Manifest.Contributes.Commands, func(c rpc.CommandDef) bool { return slices.Contains(c.Provides, capability) }) {
			out.Providers = append(out.Providers, rpc.CapabilityProvider{Plugin: st.ID, Name: st.Manifest.Name, Enabled: st.Enabled})
		}
	}
	return out
}

func (r *Router) UIEvent(ctx context.Context, pluginID string, ev json.RawMessage) (json.RawMessage, error) {
	return r.route(ctx, pluginID, "ui.event", ev, routeTimeout)
}

func (r *Router) Command(ctx context.Context, c rpc.Command, run json.RawMessage) (json.RawMessage, error) {
	timeout := routeTimeout
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}
	return r.route(ctx, c.Plugin, "command.run", run, timeout)
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
		if !st.Enabled {
			continue
		}
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
		if i := slices.IndexFunc(cs, func(c rpc.Command) bool { return c.Plugin == r.defaults[name] }); i >= 0 {
			chosen = cs[i]
		}
		out[name] = chosen
		if len(cs) > 1 && r.defaults[name] == "" {
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
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, _, ok := r.sup.awaitConn(ctx, pluginID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPluginUnavailable, pluginID)
	}
	return callConn(ctx, conn, pluginID, method, params)
}

func (r *Router) route(ctx context.Context, pluginID, method string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, waited, ok := r.sup.awaitConn(callCtx, pluginID)
	if !ok {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			r.logTimeout(ctx, pluginID, method, params, started, waited)
		}
		return nil, fmt.Errorf("%w: %s", ErrPluginUnavailable, pluginID)
	}
	sent := time.Now()
	deadline, _ := callCtx.Deadline()
	late, stopLate := context.WithDeadline(context.WithoutCancel(ctx), deadline.Add(r.sup.silence))
	out, err := callLate(callCtx, late, conn, pluginID, method, params, func(replied bool) {
		stopLate()
		if replied {
			r.sup.answered(pluginID, conn, sent)
		}
	})
	var pluginErr *rpc.PluginError
	switch {
	case err == nil || errors.As(err, &pluginErr):
		r.sup.replied(pluginID, conn)
	case errors.Is(callCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		caller, agent := r.logTimeout(ctx, pluginID, method, params, started, waited)
		if !waited {
			r.sup.timedOut(pluginID, conn, sent, caller, agent)
		}
	}
	return out, err
}

func (r *Router) logTimeout(ctx context.Context, pluginID, method string, params json.RawMessage, started time.Time, waited bool) (string, bool) {
	name, caller, agent := routedBy(ctx, method, params)
	r.sup.logger.Warn("plugin call timed out", "plugin", pluginID, "command", name, "caller", caller, "after", time.Since(started).Round(time.Millisecond), "waitedForStart", waited)
	return caller, agent
}

func routedBy(ctx context.Context, method string, params json.RawMessage) (name, caller string, agent bool) {
	name, caller = method, rpc.CallerFrom(ctx).String()
	if method != "command.run" {
		return name, caller, false
	}
	var run struct {
		ID     string `json:"id"`
		Agent  bool   `json:"agent"`
		Caller string `json:"caller"`
	}
	if json.Unmarshal(params, &run) != nil {
		return name, caller, false
	}
	if !run.Agent {
		return run.ID, caller, false
	}
	return run.ID, "agent:" + cmp.Or(run.Caller, "unknown"), true
}

func callConn(ctx context.Context, conn *jsonrpc2.Conn, pluginID, method string, params json.RawMessage) (json.RawMessage, error) {
	return callLate(ctx, ctx, conn, pluginID, method, params, func(bool) {})
}

func callLate(ctx, late context.Context, conn *jsonrpc2.Conn, pluginID, method string, params json.RawMessage, done func(replied bool)) (json.RawMessage, error) {
	type reply struct {
		out json.RawMessage
		err error
	}
	replies := make(chan reply, 1)
	go func() {
		var out json.RawMessage
		err := conn.Call(late, method, params, &out)
		var rpcErr *jsonrpc2.Error
		done(err == nil || errors.As(err, &rpcErr))
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
		return &rpc.PluginError{PluginID: pluginID, Method: method, Message: rpcErr.Message, Reason: contract.ReasonIn(rpcErr.Data)}
	case errors.Is(err, jsonrpc2.ErrClosed), errors.Is(err, context.DeadlineExceeded):
		return rpc.OutcomeUnknown(fmt.Errorf("%w: plugin %s %s: %w", ErrPluginUnavailable, pluginID, method, err))
	}
	return rpc.OutcomeUnknown(fmt.Errorf("plugin %s %s: %w", pluginID, method, err))
}
