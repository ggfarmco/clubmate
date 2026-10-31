package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
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
	"pause":  "triggers.pause",
	"resume": "triggers.resume",
	"delete": "triggers.delete",
}

type commandRequest struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args,omitempty"`
	Agent     bool            `json:"agent"`
	Caller    string          `json:"caller,omitempty"`
}

type triggerStore interface {
	save(ctx context.Context, t *Trigger) error
	remove(ctx context.Context, t *Trigger) error
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
	triggers   map[string]*Trigger
	dirty      map[string]bool
	inFlight   map[string]bool
	fired      map[string][]time.Time
	runs       sync.WaitGroup
	lastSeq    atomic.Int64
	store      triggerStore
	host       *pluginsdk.Host
	ready      chan struct{}
	now        func() time.Time
	runCommand func(ctx context.Context, req commandRequest) (json.RawMessage, error)
	sessions   func(ctx context.Context, workspace string) ([]agentcall.Session, error)
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
		dirty:    map[string]bool{},
		inFlight: map[string]bool{},
		fired:    map[string][]time.Time{},
		ready:    make(chan struct{}),
		now:      time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.projects = a.listProjects
	a.render = a.renderWorkspace
	a.notify = a.notifyPaused
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &store{h: h}
	triggers, err := st.load(ctx)
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
	a.prune(ctx)
	a.reconcileAll(ctx)
	a.renderAll(ctx)
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
		if err := a.host.StorageSet(ctx, selectedKey(ev.ProjectID), id); err != nil {
			return nil, err
		}
		a.invalidate(ev.ProjectID)
		a.flush(ctx)
		return nil, nil
	}
	cmdID, ok := uiCommands[ev.Action]
	if !ok {
		return nil, fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(idArgs{ID: id})
	if err != nil {
		return nil, err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: cmdID, ProjectID: ev.ProjectID, Args: args})
	return nil, err
}

func (a *app) reconcileAll(ctx context.Context) {
	a.mu.Lock()
	workspaces := map[string]bool{}
	for _, t := range a.triggers {
		if t.Owner != "" {
			workspaces[t.Workspace] = true
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
	for id, t := range a.triggers {
		if t.Workspace == workspace && t.Owner != "" {
			owned[id] = t.Owner
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
		t, ok := a.triggers[id]
		if !ok || t.Owner != owner {
			continue
		}
		if s, alive := agentcall.Find(sessions, owner); !alive || s.Closed {
			logError(a.drop(ctx, t))
		}
	}
}

func (a *app) prune(ctx context.Context) {
	a.mu.Lock()
	placed := map[string]string{}
	for id, t := range a.triggers {
		placed[id] = t.Workspace
	}
	a.mu.Unlock()
	if len(placed) == 0 {
		return
	}
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("prune triggers of deleted workspaces: %w", err))
		return
	}
	live := map[string]bool{}
	for _, p := range projects {
		live[p.ID] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, workspace := range placed {
		if t, ok := a.triggers[id]; ok && !live[workspace] {
			logError(a.drop(ctx, t))
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
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.triggers {
		if t.Workspace == workspace {
			logError(a.drop(ctx, t))
		}
	}
	delete(a.dirty, workspace)
}

func (a *app) owns(session string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.triggers {
		if session != "" && t.Owner == session {
			return true
		}
	}
	return false
}

func (a *app) drop(ctx context.Context, t *Trigger) error {
	delete(a.triggers, t.ID)
	delete(a.fired, t.ID)
	a.touch(t.Workspace)
	return a.store.remove(ctx, t)
}

func (a *app) renderAll(ctx context.Context) {
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("render triggers sections: %w", err))
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
	triggers := a.workspaceTriggers(workspace)
	titles := a.titles(ctx, workspace, triggers)
	if resolved := resolveSelection(triggers, selected); resolved != selected {
		if err := a.persistSelection(ctx, workspace, resolved); err != nil {
			return fmt.Errorf("render %s: %w", workspace, err)
		}
		selected = resolved
	}
	var current *Trigger
	if i := slices.IndexFunc(triggers, func(t *Trigger) bool { return t.ID == selected }); i >= 0 {
		current = triggers[i]
	}
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(triggers, selected)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, titles)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) workspaceTriggers(workspace string) []*Trigger {
	a.mu.Lock()
	defer a.mu.Unlock()
	var triggers []*Trigger
	for _, t := range a.triggers {
		if t.Workspace == workspace {
			triggers = append(triggers, clone(t))
		}
	}
	sortTriggers(triggers)
	return triggers
}

func (a *app) titles(ctx context.Context, workspace string, triggers []*Trigger) map[string]string {
	if !slices.ContainsFunc(triggers, func(t *Trigger) bool { return t.Owner != "" }) {
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

func (a *app) agentSessions(ctx context.Context, workspace string) ([]agentcall.Session, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}

func selectedKey(workspace string) string {
	return "selected/" + workspace
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "triggers:", err)
	}
}
