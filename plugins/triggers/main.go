package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

var uiCommands = map[string]string{
	"pause":  "triggers.pause",
	"resume": "triggers.resume",
	"delete": "triggers.delete",
}

type app struct {
	mu         sync.Mutex
	triggers   map[string]*Trigger
	inFlight   map[string]bool
	held       map[string]heldEvent
	fired      map[string][]time.Time
	runs       sync.WaitGroup
	lastSeq    atomic.Int64
	registry   *automation.Registry[*Trigger]
	store      automation.Storage[*Trigger]
	host       *pluginsdk.Host
	ready      chan struct{}
	now        func() time.Time
	runCommand func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error)
	sessions   func(ctx context.Context, workspace string) ([]api.Session, error)
	projects   func(ctx context.Context) ([]pluginsdk.Project, error)
	render     func(ctx context.Context, workspace string) error
	notify     func(ctx context.Context, t *Trigger)
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed})
}

func newApp() *app {
	a := &app{
		triggers: map[string]*Trigger{},
		inFlight: map[string]bool{},
		held:     map[string]heldEvent{},
		fired:    map[string][]time.Time{},
		ready:    make(chan struct{}),
		now:      time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.projects = a.listProjects
	a.render = a.renderWorkspace
	a.notify = a.notifyPaused
	a.registry = &automation.Registry[*Trigger]{
		Mu:             &a.mu,
		Items:          a.triggers,
		Noun:           "trigger",
		Sessions:       func(ctx context.Context, ws string) ([]api.Session, error) { return a.sessions(ctx, ws) },
		Projects:       func(ctx context.Context) ([]pluginsdk.Project, error) { return a.projects(ctx) },
		Render:         func(ctx context.Context, ws string) error { return a.render(ctx, ws) },
		Save:           func(ctx context.Context, t *Trigger) error { return a.store.Save(ctx, t) },
		Drop:           a.drop,
		SessionDeleted: a.unhold,
		Log:            logError,
	}
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &automation.Store[Trigger, *Trigger]{Host: h, Prefix: triggerPrefix, Noun: "trigger", Log: logError}
	triggers, err := st.Load(ctx)
	if err != nil {
		return err
	}
	a.store = st
	a.mu.Lock()
	for _, t := range triggers {
		a.triggers[t.ID] = t
	}
	a.mu.Unlock()
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	a.lastSeq.Store(ready.Seq)
	if _, err := h.Subscribe(ctx, anyTopic, ready.Seq); err != nil {
		return err
	}
	close(a.ready)
	a.registry.Prune(ctx)
	a.registry.ReconcileAll(ctx)
	a.registry.RenderAll(ctx)
	return nil
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return
	}
	a.lastSeq.Store(ev.Seq)
	a.dispatch(ctx, ev)
	a.registry.BusEvent(ctx, ev)
}

func (a *app) busClosed(ctx context.Context, h *pluginsdk.Host, _ string, cause error) {
	logError(fmt.Errorf("bus subscription closed: %w", cause))
	if _, err := h.Subscribe(ctx, anyTopic, a.lastSeq.Load()); err != nil {
		logError(fmt.Errorf("resubscribe: %w", err))
	}
}

func (a *app) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ev.Slot != sidebarSlot {
		return nil, nil
	}
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return nil, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if ev.Action == "select" {
		if err := a.host.StorageSet(ctx, automation.SelectedKey(ev.ProjectID), id); err != nil {
			return nil, err
		}
		a.registry.Invalidate(ev.ProjectID)
		a.registry.Flush(ctx)
		return nil, nil
	}
	cmdID, ok := uiCommands[ev.Action]
	if !ok {
		return nil, fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(automation.IDArgs{ID: id})
	if err != nil {
		return nil, err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: cmdID, ProjectID: ev.ProjectID, Args: args})
	return nil, err
}

func (a *app) renderWorkspace(ctx context.Context, workspace string) error {
	triggers := a.registry.InWorkspace(workspace)
	current, selected, err := automation.Selection(ctx, a.host, workspace, triggers)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	titles := a.registry.Titles(ctx, workspace, triggers)
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(triggers, selected)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, titles)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) callCommand(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
	return automation.CallCommand(ctx, a.host, req)
}

func (a *app) listProjects(ctx context.Context) ([]pluginsdk.Project, error) {
	return a.host.ListProjects(ctx)
}

func (a *app) agentSessions(ctx context.Context, workspace string) ([]api.Session, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}
