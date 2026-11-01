package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
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
	collapsed  map[string]map[string]bool
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
	workspaces func(ctx context.Context) ([]pluginsdk.Workspace, error)
	render     func(ctx context.Context, workspace string) error
	notify     func(ctx context.Context, t *Trigger)
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed})
}

func newApp() *app {
	a := &app{
		triggers:  map[string]*Trigger{},
		inFlight:  map[string]bool{},
		collapsed: map[string]map[string]bool{},
		held:      map[string]heldEvent{},
		fired:     map[string][]time.Time{},
		ready:     make(chan struct{}),
		now:       time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.workspaces = a.listWorkspaces
	a.render = a.renderWorkspace
	a.notify = a.notifyPaused
	a.registry = &automation.Registry[*Trigger]{
		Mu:             &a.mu,
		Items:          a.triggers,
		Noun:           "trigger",
		Sessions:       func(ctx context.Context, ws string) ([]api.Session, error) { return a.sessions(ctx, ws) },
		Workspaces:     func(ctx context.Context) ([]pluginsdk.Workspace, error) { return a.workspaces(ctx) },
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
	var id string
	switch ev.Slot {
	case mainSlot:
		target, ok := automation.ActionTarget(ev.NodeID, ev.Action)
		if !ok {
			return nil, nil
		}
		id = target
	case sidebarSlot:
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return nil, fmt.Errorf("%s value: %w", ev.Action, err)
		}
	default:
		return nil, nil
	}
	switch {
	case ev.Action == "expand" || ev.Action == "collapse":
		a.fold(ev.WorkspaceID, id, ev.Action == "collapse")
		a.registry.Invalidate(ev.WorkspaceID)
		a.registry.Flush(ctx)
		return nil, nil
	case strings.HasPrefix(id, groupPrefix):
		return nil, nil
	case automation.IsToggle(ev.Action):
		if err := automation.Toggle(ctx, a.host, ev.Action, ev.WorkspaceID, id); err != nil {
			return nil, err
		}
		a.registry.Invalidate(ev.WorkspaceID)
		a.registry.Flush(ctx)
		return nil, nil
	case ev.Action == "select":
		if err := a.host.StorageSet(ctx, automation.SelectedKey(ev.WorkspaceID), id); err != nil {
			return nil, err
		}
		a.registry.Invalidate(ev.WorkspaceID)
		a.registry.Flush(ctx)
		return nil, nil
	case ev.Action == automation.ActRun:
		return nil, a.runNow(context.WithoutCancel(ctx), ev.WorkspaceID, id)
	case ev.Action == actGraph:
		return nil, a.openGraph(ctx, ev.WorkspaceID, id)
	}
	cmdID, ok := uiCommands[ev.Action]
	if !ok {
		return nil, fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(automation.IDArgs{ID: id})
	if err != nil {
		return nil, err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: cmdID, WorkspaceID: ev.WorkspaceID, Args: args})
	return nil, err
}

func (a *app) renderWorkspace(ctx context.Context, workspace string) error {
	triggers := a.registry.InWorkspace(workspace)
	current, selected, err := automation.Selection(ctx, a.host, workspace, triggers)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	open, err := automation.ReadOpen(ctx, a.host, workspace, selected)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	titles := a.registry.Titles(ctx, workspace, triggers)
	for _, t := range triggers {
		if t.Run != "" && t.RunTitle != "" {
			if titles == nil {
				titles = map[string]string{}
			}
			titles[t.Run] = t.RunTitle
		}
	}
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(triggers, selected, a.folds(workspace), titles)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, titles, open)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) fold(workspace, id string, collapsed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.collapsed[workspace] == nil {
		a.collapsed[workspace] = map[string]bool{}
	}
	a.collapsed[workspace][id] = collapsed
}

func (a *app) folds(workspace string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return maps.Clone(a.collapsed[workspace])
}

func (a *app) runNow(ctx context.Context, workspace, id string) error {
	a.mu.Lock()
	t, ok := a.triggers[id]
	if !ok || t.Workspace != workspace {
		a.mu.Unlock()
		return fmt.Errorf("trigger %q not found", id)
	}
	t = t.Clone()
	if a.inFlight[id] {
		a.mu.Unlock()
		return fmt.Errorf("trigger %q is already running", t.Name)
	}
	a.inFlight[id] = true
	a.mu.Unlock()
	a.runs.Go(func() { logError(a.settle(ctx, id, a.fire(ctx, t, pluginsdk.Event{Topic: manualTopic}), false)) })
	return nil
}

func (a *app) openGraph(ctx context.Context, workspace, id string) error {
	a.mu.Lock()
	var run string
	if t, ok := a.triggers[id]; ok && t.Workspace == workspace {
		run = t.Run
	}
	a.mu.Unlock()
	if run == "" {
		return fmt.Errorf("trigger %q was not created by a graph", id)
	}
	if err := a.host.RunCommand(ctx, "graphs.reveal", workspace, automation.IDArgs{ID: run}, nil); err != nil {
		return fmt.Errorf("open graph run %s: %w", run, err)
	}
	return nil
}

func (a *app) callCommand(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
	return automation.CallCommand(ctx, a.host, req)
}

func (a *app) listWorkspaces(ctx context.Context) ([]pluginsdk.Workspace, error) {
	return a.host.ListWorkspaces(ctx)
}

func (a *app) agentSessions(ctx context.Context, workspace string) ([]api.Session, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}
