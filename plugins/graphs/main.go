package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
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
	changeDeleted  = "deleted"
	changeCreated  = "created"
	reasonBudget   = "turn budget of %d reached"
	reasonCaller   = "the session that ran it is closed"
	reasonStopped  = "stopped"
	reasonKickoff  = "start failed: %v"
	callTimeout    = 30 * time.Second
	agentsTopics   = "agents.*"
	projectTopics  = "project.*"
	selectedPrefix = "selected/"
)

var errNotYours = errors.New("this run was not started by this agent session or its child sessions")

type runStore interface {
	save(ctx context.Context, r *Run) error
	remove(ctx context.Context, r *Run) error
}

type app struct {
	mu         sync.Mutex
	renderMu   sync.Mutex
	runs       map[string]*Run
	dirty      map[string]bool
	lastSeq    atomic.Int64
	subs       map[string]string
	store      runStore
	host       *pluginsdk.Host
	ready      chan struct{}
	now        func() time.Time
	runCommand func(ctx context.Context, req commandRequest) (json.RawMessage, error)
	sessions   func(ctx context.Context, workspace string) ([]agentcall.Session, error)
	project    func(ctx context.Context, workspace string) (pluginsdk.Project, error)
	projects   func(ctx context.Context) ([]pluginsdk.Project, error)
	render     func(ctx context.Context, workspace string) error
}

type changedPayload struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Kind      string `json:"kind"`
}

type turnPayload struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
}

type createdPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed})
}

func newApp() *app {
	a := &app{
		runs:  map[string]*Run{},
		dirty: map[string]bool{},
		subs:  map[string]string{},
		ready: make(chan struct{}),
		now:   time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.project = a.getProject
	a.projects = a.listProjects
	a.render = a.renderWorkspace
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &store{h: h}
	runs, err := st.load(ctx)
	if err != nil {
		return err
	}
	a.store = st
	a.mu.Lock()
	for _, r := range runs {
		a.runs[r.ID] = r
	}
	a.mu.Unlock()
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	a.lastSeq.Store(ready.Seq)
	for _, pattern := range []string{agentsTopics, projectTopics} {
		if err := a.subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	close(a.ready)
	a.renderAll(ctx)
	return nil
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return
	}
	a.lastSeq.Store(max(a.lastSeq.Load(), ev.Seq))
	switch ev.Topic {
	case turnTopic:
		var p turnPayload
		if decode(ev, &p) {
			a.countTurn(ctx, p)
		}
	case agentsChanged:
		var p changedPayload
		if decode(ev, &p) {
			a.sessionChanged(ctx, p)
		}
	case agentsReady:
		a.renderAll(ctx)
	case projectCreated:
		var p createdPayload
		if decode(ev, &p) {
			a.invalidate(p.Project.ID)
			a.flush(ctx)
		}
	case projectDeleted:
		var p deletedPayload
		if decode(ev, &p) {
			a.dropWorkspace(ctx, p.ID)
		}
	}
}

func (a *app) busClosed(ctx context.Context, _ *pluginsdk.Host, sub string, cause error) {
	a.mu.Lock()
	pattern, ok := a.subs[sub]
	delete(a.subs, sub)
	a.mu.Unlock()
	logError(fmt.Errorf("bus subscription %s closed: %w", pattern, cause))
	if ok {
		logError(a.subscribe(ctx, pattern, a.lastSeq.Load()))
	}
}

func (a *app) subscribe(ctx context.Context, pattern string, after int64) error {
	sub, err := a.host.Subscribe(ctx, pattern, after)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", pattern, err)
	}
	a.mu.Lock()
	a.subs[sub] = pattern
	a.mu.Unlock()
	return nil
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
	cmd := pluginsdk.CommandRun{ProjectID: ev.ProjectID}
	switch ev.Action {
	case "select":
		if err := a.host.StorageSet(ctx, selectedPrefix+ev.ProjectID, id); err != nil {
			return nil, err
		}
		a.invalidate(ev.ProjectID)
		a.flush(ctx)
		return nil, nil
	case "stop":
		cmd.ID = "graphs.stop"
	case "remove":
		cmd.ID = "graphs.delete"
	default:
		return nil, fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(idArgs{ID: id})
	if err != nil {
		return nil, err
	}
	cmd.Args = args
	_, err = a.command(ctx, nil, cmd)
	return nil, err
}

func (a *app) countTurn(ctx context.Context, p turnPayload) {
	a.mu.Lock()
	var spent []*Run
	for _, r := range a.runs {
		if r.Workspace != p.Workspace || r.Status != statusRunning || r.nodeBySession(p.Session) == nil {
			continue
		}
		r.Turns++
		a.touch(r.Workspace)
		if r.Turns >= r.MaxTurns {
			r.stop(fmt.Sprintf(reasonBudget, r.MaxTurns), a.now())
			spent = append(spent, cloneRun(r))
		}
		logError(a.store.save(ctx, r))
	}
	a.mu.Unlock()
	for _, r := range spent {
		x := &runner{call: a.runCommand, run: r}
		logError(errors.Join(x.unwind(ctx)...))
	}
	a.flush(ctx)
}

func (a *app) sessionChanged(ctx context.Context, p changedPayload) {
	a.mu.Lock()
	var callers, involved bool
	for _, r := range a.runs {
		if r.Workspace != p.Workspace {
			continue
		}
		if r.nodeBySession(p.Session) != nil {
			involved = true
		}
		if r.Status == statusRunning && r.Caller == p.Session {
			callers = true
		}
	}
	if involved || callers {
		a.touch(p.Workspace)
	}
	a.mu.Unlock()
	if callers {
		a.checkCaller(ctx, p)
	}
	if p.Kind == changeCreated {
		a.followHandoffs(ctx, p.Workspace)
	}
	a.flush(ctx)
}

func (a *app) followHandoffs(ctx context.Context, workspace string) {
	if !a.hasLiveRun(workspace) {
		return
	}
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		logError(err)
		return
	}
	next := map[string]string{}
	for _, s := range sessions {
		if s.HandoffFrom != "" {
			next[s.HandoffFrom] = s.ID
		}
	}
	a.mu.Lock()
	var moved []*Run
	for _, r := range a.runs {
		if r.Workspace != workspace || r.Status != statusRunning || r.Spec == nil {
			continue
		}
		changed := false
		for i := range r.Nodes {
			n := &r.Nodes[i]
			for range len(next) {
				to, ok := next[n.Session]
				if !ok {
					break
				}
				n.Session, n.HandedOff, changed = to, true, true
			}
		}
		if changed {
			a.touch(workspace)
			logError(a.store.save(ctx, r))
			moved = append(moved, cloneRun(r))
		}
	}
	a.mu.Unlock()
	for _, r := range moved {
		x := &runner{call: a.runCommand, run: r}
		logError(errors.Join(x.repoint(ctx)...))
	}
}

func (a *app) hasLiveRun(workspace string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.runs {
		if r.Workspace == workspace && r.Status == statusRunning && r.Spec != nil {
			return true
		}
	}
	return false
}

func (a *app) checkCaller(ctx context.Context, p changedPayload) {
	if p.Kind != changeDeleted {
		sessions, err := a.sessions(ctx, p.Workspace)
		if err != nil {
			logError(err)
			return
		}
		if s, ok := agentcall.Find(sessions, p.Session); ok && !s.Closed {
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.runs {
		if r.Workspace == p.Workspace && r.Status == statusRunning && r.Caller == p.Session {
			r.stop(reasonCaller, a.now())
			logError(a.store.save(ctx, r))
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
	for id, r := range a.runs {
		if r.Workspace == workspace {
			delete(a.runs, id)
			logError(a.store.remove(ctx, r))
		}
	}
	delete(a.dirty, workspace)
}

func (a *app) graphDirs(ctx context.Context, workspace string, agent bool, caller string) ([]string, error) {
	if agent && caller != "" {
		sessions, err := a.sessions(ctx, workspace)
		if err != nil {
			return nil, err
		}
		if s, ok := agentcall.Find(sessions, caller); ok && s.Worktree != "" {
			return []string{s.Worktree}, nil
		}
	}
	p, err := a.project(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if len(p.Roots) > 0 {
		return p.Roots, nil
	}
	return p.Repos, nil
}

func (a *app) authorize(ctx context.Context, cmd pluginsdk.CommandRun, r *Run) error {
	if !cmd.Agent {
		return nil
	}
	if cmd.Caller == "" {
		return errNotYours
	}
	if r.Caller == cmd.Caller {
		return nil
	}
	sessions, err := a.sessions(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if agentcall.Descends(r.Caller, cmd.Caller, agentcall.Parents(sessions)) {
		return nil
	}
	return errNotYours
}

func (a *app) renderAll(ctx context.Context) {
	projects, err := a.projects(ctx)
	if err != nil {
		logError(fmt.Errorf("render graphs sections: %w", err))
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
	if _, err := a.host.StorageGet(ctx, selectedPrefix+workspace, &selected); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	runs := a.workspaceRuns(workspace)
	var statuses map[string]agentcall.Session
	if len(runs) > 0 {
		statuses = a.sessionMap(ctx, workspace)
	}
	if i := slices.IndexFunc(runs, func(r *Run) bool { return r.ID == selected }); i < 0 {
		selected = ""
		if len(runs) > 0 {
			selected = runs[0].ID
		}
	}
	var current *Run
	if i := slices.IndexFunc(runs, func(r *Run) bool { return r.ID == selected }); i >= 0 {
		current = runs[i]
	}
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(runs, selected, statuses)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, statuses)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) sessionMap(ctx context.Context, workspace string) map[string]agentcall.Session {
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		logError(err)
		return nil
	}
	m := make(map[string]agentcall.Session, len(sessions))
	for _, s := range sessions {
		m[s.ID] = s
	}
	return m
}

func (a *app) workspaceRuns(workspace string) []*Run {
	a.mu.Lock()
	defer a.mu.Unlock()
	var runs []*Run
	for _, r := range a.runs {
		if r.Workspace == workspace {
			runs = append(runs, cloneRun(r))
		}
	}
	sortRuns(runs)
	return runs
}

func (a *app) callCommand(ctx context.Context, req commandRequest) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := a.host.Call(ctx, "command.run", req, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *app) agentSessions(ctx context.Context, workspace string) ([]agentcall.Session, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}

func (a *app) getProject(ctx context.Context, workspace string) (pluginsdk.Project, error) {
	return a.host.GetProject(ctx, workspace)
}

func (a *app) listProjects(ctx context.Context) ([]pluginsdk.Project, error) {
	return a.host.ListProjects(ctx)
}

func decode(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		logError(fmt.Errorf("decode %s: %w", ev.Topic, err))
		return false
	}
	return true
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "graphs:", strings.TrimSpace(err.Error()))
	}
}
