package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	agentsChanged  = "agents.changed"
	agentsReady    = "agents.ready"
	projectCreated = "project.created"
	projectDeleted = "project.deleted"
	changeStatus   = "status"
	changeDeleted  = "deleted"
)

var uiCommands = map[string]string{
	"pause":  "cron.pause",
	"resume": "cron.resume",
	"run":    "cron.run",
	"delete": "cron.delete",
}

type commandRequest struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args,omitempty"`
	Agent     bool            `json:"agent"`
	Caller    string          `json:"caller,omitempty"`
}

type jobStore interface {
	save(ctx context.Context, j *Job) error
	remove(ctx context.Context, j *Job) error
}

type changedPayload struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Kind      string `json:"kind"`
}

type createdPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type app struct {
	mu         sync.Mutex
	renderMu   sync.Mutex
	jobs       map[string]*Job
	dirty      map[string]bool
	inFlight   map[string]bool
	runs       sync.WaitGroup
	store      jobStore
	host       *pluginsdk.Host
	ready      chan struct{}
	poke       chan struct{}
	now        func() time.Time
	runCommand func(ctx context.Context, req commandRequest) (json.RawMessage, error)
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
		dirty:    map[string]bool{},
		ready:    make(chan struct{}),
		poke:     make(chan struct{}, 1),
		now:      time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.projects = a.listProjects
	a.render = a.renderWorkspace
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &store{h: h}
	jobs, err := st.load(ctx)
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
	for _, topic := range []string{agentsChanged, agentsReady, projectCreated, projectDeleted} {
		if _, err := h.Subscribe(ctx, topic, ready.Seq); err != nil {
			return err
		}
	}
	close(a.ready)
	a.prune(ctx)
	go a.loop(ctx)
	a.reconcileAll(ctx)
	a.renderAll(ctx)
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
		a.flush(ctx)
	}
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return
	}
	switch ev.Topic {
	case agentsChanged:
		var p changedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(fmt.Errorf("decode %s: %w", ev.Topic, err))
			return
		}
		if (p.Kind == changeStatus || p.Kind == changeDeleted) && a.owns(p.Session) {
			a.reconcile(ctx, p.Workspace)
			a.flush(ctx)
		}
	case agentsReady:
		a.reconcileAll(ctx)
		a.renderAll(ctx)
	case projectCreated:
		var p createdPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(fmt.Errorf("decode %s: %w", ev.Topic, err))
			return
		}
		a.invalidate(p.Project.ID)
		a.flush(ctx)
	case projectDeleted:
		var p deletedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			logError(fmt.Errorf("decode %s: %w", ev.Topic, err))
			return
		}
		a.dropWorkspace(ctx, p.ID)
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
		if err := a.host.StorageSet(ctx, selectedKey(ev.ProjectID), id); err != nil {
			return nil, err
		}
		a.invalidate(ev.ProjectID)
		a.flush(ctx)
		return nil, nil
	}
	cmdID, ok := uiCommands[ev.Action]
	if !ok {
		return nil, fmt.Errorf("%w: action %q", errUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(idArgs{ID: id})
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

func (a *app) reconcileAll(ctx context.Context) {
	a.mu.Lock()
	workspaces := map[string]bool{}
	for _, j := range a.jobs {
		if j.Owner != "" {
			workspaces[j.Workspace] = true
		}
	}
	a.mu.Unlock()
	for ws := range workspaces {
		a.reconcile(ctx, ws)
	}
}

func (a *app) reconcile(ctx context.Context, workspace string) {
	a.mu.Lock()
	owned := map[string]string{}
	for id, j := range a.jobs {
		if j.Workspace == workspace && j.Owner != "" {
			owned[id] = j.Owner
		}
	}
	a.mu.Unlock()
	if len(owned) == 0 {
		return
	}
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		logError(err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, owner := range owned {
		j, ok := a.jobs[id]
		if !ok || j.Owner != owner {
			continue
		}
		if s, alive := findSession(sessions, owner); !alive || s.Closed {
			logError(a.drop(ctx, j))
		}
	}
}

func (a *app) prune(ctx context.Context) {
	a.mu.Lock()
	placed := map[string]string{}
	for id, j := range a.jobs {
		if j.Workspace != "" {
			placed[id] = j.Workspace
		}
	}
	a.mu.Unlock()
	if len(placed) == 0 {
		return
	}
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("prune jobs of deleted workspaces: %w", err))
		return
	}
	live := map[string]bool{}
	for _, p := range projects {
		live[p.ID] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, workspace := range placed {
		if j, ok := a.jobs[id]; ok && !live[workspace] {
			logError(a.drop(ctx, j))
			delete(a.dirty, workspace)
		}
	}
}

func (a *app) dropWorkspace(ctx context.Context, workspace string) {
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("check deleted project %s: %w", workspace, err))
		return
	}
	if slices.ContainsFunc(projects, func(p pluginsdk.Project) bool { return p.ID == workspace }) {
		logError(fmt.Errorf("ignore %s for live project %s", projectDeleted, workspace))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, j := range a.jobs {
		if j.Workspace == workspace {
			logError(a.drop(ctx, j))
		}
	}
	delete(a.dirty, workspace)
}

func (a *app) owns(session string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, j := range a.jobs {
		if session != "" && j.Owner == session {
			return true
		}
	}
	return false
}

func (a *app) renderAll(ctx context.Context) {
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("render cron sections: %w", err))
		return
	}
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	a.invalidate(ids...)
	a.flush(ctx)
}

func (a *app) invalidate(workspaces ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ws := range workspaces {
		a.touch(ws)
	}
}

func (a *app) touch(workspace string) {
	if workspace != "" {
		a.dirty[workspace] = true
	}
}

func (a *app) flush(ctx context.Context) {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	a.mu.Lock()
	dirty := a.dirty
	a.dirty = map[string]bool{}
	a.mu.Unlock()
	for ws := range dirty {
		logError(a.render(ctx, ws))
	}
}

func (a *app) renderWorkspace(ctx context.Context, workspace string) error {
	var selected string
	if _, err := a.host.StorageGet(ctx, selectedKey(workspace), &selected); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	jobs := a.workspaceJobs(workspace)
	titles := a.titles(ctx, workspace, jobs)
	if resolved := resolveSelection(jobs, selected); resolved != selected {
		if err := a.persistSelection(ctx, workspace, resolved); err != nil {
			return fmt.Errorf("render %s: %w", workspace, err)
		}
		selected = resolved
	}
	var current *Job
	if i := slices.IndexFunc(jobs, func(j *Job) bool { return j.ID == selected }); i >= 0 {
		current = jobs[i]
	}
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(jobs, titles, selected, a.now())); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, titles)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) workspaceJobs(workspace string) []*Job {
	a.mu.Lock()
	defer a.mu.Unlock()
	var jobs []*Job
	for _, j := range a.jobs {
		if j.Workspace == workspace {
			jobs = append(jobs, clone(j))
		}
	}
	sortJobs(jobs)
	return jobs
}

func (a *app) titles(ctx context.Context, workspace string, jobs []*Job) map[string]string {
	if !slices.ContainsFunc(jobs, func(j *Job) bool { return j.Owner != "" }) {
		return nil
	}
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		return nil
	}
	titles := make(map[string]string, len(sessions))
	for _, s := range sessions {
		titles[s.ID] = s.Title
	}
	return titles
}

func (a *app) persistSelection(ctx context.Context, workspace, id string) error {
	if id == "" {
		return a.host.StorageDelete(ctx, selectedKey(workspace))
	}
	return a.host.StorageSet(ctx, selectedKey(workspace), id)
}

func (a *app) callCommand(ctx context.Context, req commandRequest) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := a.host.Call(ctx, "command.run", req, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *app) listProjects(ctx context.Context) ([]pluginsdk.Project, error) {
	return a.host.ListProjects(ctx)
}

func (a *app) agentSessions(ctx context.Context, workspace string) ([]agentSession, error) {
	var sessions []agentSession
	if err := a.host.RunCommand(ctx, "agents.sessions", workspace, struct{}{}, &sessions); err != nil {
		return nil, fmt.Errorf("agent sessions of %s: %w", workspace, err)
	}
	return sessions, nil
}

func (a *app) wake() {
	select {
	case a.poke <- struct{}{}:
	default:
	}
}

func selectedKey(workspace string) string {
	return "selected/" + workspace
}
