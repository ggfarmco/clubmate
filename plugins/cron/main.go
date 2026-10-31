package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	jobPrefix = "job/"
	idPrefix  = "cj"
)

var uiCommands = map[string]string{
	"pause":  "cron.pause",
	"resume": "cron.resume",
	"run":    "cron.run",
	"delete": "cron.delete",
}

type app struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	inFlight   map[string]bool
	runs       sync.WaitGroup
	registry   *automation.Registry[*Job]
	store      automation.Storage[*Job]
	host       *pluginsdk.Host
	ready      chan struct{}
	poke       chan struct{}
	now        func() time.Time
	runCommand func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error)
	sessions   func(ctx context.Context, workspace string) ([]agentSession, error)
	projects   func(ctx context.Context) ([]pluginsdk.Project, error)
	render     func(ctx context.Context, workspace string) error
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func newApp() *app {
	a := &app{
		jobs:     map[string]*Job{},
		inFlight: map[string]bool{},
		ready:    make(chan struct{}),
		poke:     make(chan struct{}, 1),
		now:      time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.projects = a.listProjects
	a.render = a.renderWorkspace
	a.registry = &automation.Registry[*Job]{
		Mu:       &a.mu,
		Items:    a.jobs,
		Noun:     "job",
		Sessions: func(ctx context.Context, ws string) ([]agentSession, error) { return a.sessions(ctx, ws) },
		Projects: func(ctx context.Context) ([]pluginsdk.Project, error) { return a.projects(ctx) },
		Render:   func(ctx context.Context, ws string) error { return a.render(ctx, ws) },
		Save:     func(ctx context.Context, j *Job) error { return a.store.Save(ctx, j) },
		Drop:     a.drop,
		Changed:  a.wake,
		Log:      logError,
	}
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &automation.Store[Job, *Job]{Host: h, Prefix: jobPrefix, Noun: "job", Log: logError}
	jobs, err := st.Load(ctx)
	if err != nil {
		return err
	}
	a.store = st
	a.mu.Lock()
	for _, j := range jobs {
		a.jobs[j.ID] = j
	}
	a.mu.Unlock()
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	topics := []string{automation.TopicAgentsChanged, automation.TopicAgentsReady, automation.TopicProjectCreated, automation.TopicProjectDeleted}
	for _, topic := range topics {
		if _, err := h.Subscribe(ctx, topic, ready.Seq); err != nil {
			return err
		}
	}
	close(a.ready)
	a.registry.Prune(ctx)
	go a.loop(ctx)
	a.registry.ReconcileAll(ctx)
	a.registry.RenderAll(ctx)
	return nil
}

func (a *app) loop(ctx context.Context) {
	timer := time.NewTimer(a.tick(ctx))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-a.poke:
		}
		timer.Reset(a.tick(ctx))
		a.registry.Flush(ctx)
	}
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return
	}
	a.registry.BusEvent(ctx, ev)
}

func (a *app) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ev.Slot == mainSlot && ev.Action == actReveal {
		return nil, a.revealOwner(ctx, ev.ProjectID, strings.TrimSuffix(ev.NodeID, ownerNode))
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
	cmd := pluginsdk.CommandRun{ID: cmdID, ProjectID: ev.ProjectID, Args: args}
	if ev.Action == "run" {
		ctx = context.WithoutCancel(ctx)
		a.runs.Go(func() {
			_, err := a.command(ctx, nil, cmd)
			logError(err)
		})
		return nil, nil
	}
	_, err = a.command(ctx, nil, cmd)
	return nil, err
}

func (a *app) revealOwner(ctx context.Context, workspace, id string) error {
	a.mu.Lock()
	var owner string
	if j, ok := a.jobs[id]; ok && j.Workspace == workspace {
		owner = j.Owner
	}
	a.mu.Unlock()
	if owner == "" {
		return fmt.Errorf("job %q has no owner session", id)
	}
	return agentcall.Reveal(ctx, a.host, workspace, owner)
}

func (a *app) renderWorkspace(ctx context.Context, workspace string) error {
	jobs := a.registry.InWorkspace(workspace)
	current, selected, err := automation.Selection(ctx, a.host, workspace, jobs)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	titles := a.registry.Titles(ctx, workspace, jobs)
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(jobs, titles, selected, a.now())); err != nil {
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

func (a *app) agentSessions(ctx context.Context, workspace string) ([]agentSession, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}

func (a *app) wake() {
	select {
	case a.poke <- struct{}{}:
	default:
	}
}
