package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	jobPrefix = "job/"
	idPrefix  = "cj"
)

var uiCommands = map[string]string{
	"pause":     "cron.pause",
	"resume":    "cron.resume",
	"run":       "cron.run",
	"runMissed": "cron.run",
	"delete":    "cron.delete",
}

type app struct {
	mu            sync.Mutex
	jobs          map[string]*Job
	inFlight      map[string]bool
	drafts        map[string]draft
	sentReminders map[string][]reminder
	runs          sync.WaitGroup
	runRows       automation.Runs
	registry      *automation.Registry[*Job]
	store         automation.Storage[*Job]
	host          *pluginsdk.Host
	ready         chan struct{}
	poke          chan struct{}
	now           func() time.Time
	runCommand    func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error)
	sessions      func(ctx context.Context, workspace string) ([]agentSession, error)
	workspaces    func(ctx context.Context) ([]pluginsdk.Workspace, error)
	render        func(ctx context.Context, workspace string) error
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func newApp() *app {
	a := &app{
		jobs:          map[string]*Job{},
		inFlight:      map[string]bool{},
		drafts:        map[string]draft{},
		sentReminders: map[string][]reminder{},
		ready:         make(chan struct{}),
		poke:          make(chan struct{}, 1),
		now:           wallNow,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.workspaces = a.listWorkspaces
	a.render = a.renderWorkspace
	a.registry = &automation.Registry[*Job]{
		Mu:         &a.mu,
		Items:      a.jobs,
		Noun:       "job",
		Sessions:   func(ctx context.Context, ws string) ([]agentSession, error) { return a.sessions(ctx, ws) },
		Workspaces: func(ctx context.Context) ([]pluginsdk.Workspace, error) { return a.workspaces(ctx) },
		Commands:   func(ctx context.Context) (map[string]string, error) { return agentcall.CommandTitles(ctx, a.host) },
		Render: func(ctx context.Context, ws string) error {
			err := a.render(ctx, ws)
			a.feedReminders(ctx, ws)
			return err
		},
		Save:    func(ctx context.Context, j *Job) error { return a.store.Save(ctx, j) },
		Drop:    a.drop,
		Changed: a.wake,
		Notify:  automation.HostNotify(func() *pluginsdk.Host { return a.host }),
		Log:     logError,
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
	topics := []string{api.TopicChanged, api.TopicReady, automation.TopicWorkspaceCreated, automation.TopicWorkspaceDeleted}
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
	if ev.Topic == api.TopicReady {
		a.forgetReminders()
	}
	a.registry.BusEvent(ctx, ev)
	if ev.Topic == automation.TopicWorkspaceDeleted {
		a.forgetWorkspaceReminders(ev.Payload)
	}
}

func (a *app) forgetWorkspaceReminders(payload json.RawMessage) {
	var p contract.WorkspaceDeleted
	if json.Unmarshal(payload, &p) != nil || len(a.reminders(p.Workspace)) > 0 {
		return
	}
	a.mu.Lock()
	delete(a.sentReminders, p.Workspace)
	a.mu.Unlock()
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
		if ev.NodeID == kindsID {
			return nil, a.askFromRow(ctx, ev)
		}
		if ev.NodeID == automation.AskID {
			return nil, a.askAgent(ctx, ev.WorkspaceID, "")
		}
		if _, strip := automation.ActionTarget(ev.NodeID, stripNode); ev.NodeID == automation.HistoryID || strip {
			if ev.Action == actShowLog {
				return nil, a.showLog(ctx, ev)
			}
			if err := a.runRows.Handle(ctx, a.host, ev); err != nil {
				return nil, err
			}
			if strip && ev.Action == "select" {
				if err := a.showRuns(ctx, ev.WorkspaceID); err != nil {
					return nil, err
				}
			}
			a.registry.Invalidate(ev.WorkspaceID)
			a.registry.Flush(ctx)
			return nil, nil
		}
		if ev.NodeID == doesFlowID {
			var card string
			_ = json.Unmarshal(ev.Value, &card)
			if ev.Action != actOpen || !strings.HasPrefix(card, "agt_") {
				return nil, nil
			}
			return nil, agentcall.Reveal(ctx, a.host, ev.WorkspaceID, card)
		}
		target, ok := automation.ActionTarget(ev.NodeID, ev.Action)
		if !ok {
			return nil, nil
		}
		if ev.Action == automation.ActReveal {
			return a.revealOwner(ctx, ev.WorkspaceID, target)
		}
		id = target
	case sidebarSlot:
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return nil, fmt.Errorf("%s value: %w", ev.Action, err)
		}
	default:
		return nil, nil
	}
	if ev.Action == actSave {
		return nil, a.save(ctx, ev, id)
	}
	if automation.IsToggle(ev.Action) {
		if ev.Action == automation.ActEdit {
			a.mu.Lock()
			delete(a.drafts, ev.WorkspaceID)
			a.mu.Unlock()
		}
		if err := automation.Toggle(ctx, a.host, ev.Action, ev.WorkspaceID, id); err != nil {
			return nil, err
		}
		a.registry.Invalidate(ev.WorkspaceID)
		a.registry.Flush(ctx)
		return nil, nil
	}
	if ev.Action == actDuplicate {
		return nil, a.duplicate(ctx, ev.WorkspaceID, id)
	}
	if ev.Action == actMissed {
		return nil, a.setMissed(ctx, ev, id)
	}
	if ev.Action == "select" {
		if err := a.host.StorageSet(ctx, automation.SelectedKey(ev.WorkspaceID), id); err != nil {
			return nil, err
		}
		if err := automation.Close(ctx, a.host, automation.ActEdit, ev.WorkspaceID); err != nil {
			return nil, err
		}
		a.mu.Lock()
		delete(a.drafts, ev.WorkspaceID)
		a.mu.Unlock()
		a.registry.Invalidate(ev.WorkspaceID)
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
	cmd := pluginsdk.CommandRun{ID: cmdID, WorkspaceID: ev.WorkspaceID, Args: args}
	if cmdID == "cron.run" {
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

func (a *app) showLog(ctx context.Context, ev pluginsdk.UIEvent) error {
	var job string
	if _, err := a.host.StorageGet(ctx, automation.SelectedKey(ev.WorkspaceID), &job); err != nil {
		return err
	}
	ev.Action = "select"
	if err := a.runRows.Handle(ctx, a.host, ev); err != nil {
		return err
	}
	if err := automation.Show(ctx, a.host, automation.ActRunJSON, ev.WorkspaceID, job); err != nil {
		return err
	}
	a.registry.Invalidate(ev.WorkspaceID)
	a.registry.Flush(ctx)
	return nil
}

func (a *app) showRuns(ctx context.Context, workspace string) error {
	var job string
	if _, err := a.host.StorageGet(ctx, automation.SelectedKey(workspace), &job); err != nil {
		return err
	}
	return automation.Show(ctx, a.host, automation.ActRuns, workspace, job)
}

func (a *app) setMissed(ctx context.Context, ev pluginsdk.UIEvent, id string) error {
	var policy string
	if err := json.Unmarshal(ev.Value, &policy); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if policy != missedOnce && policy != missedSkip {
		return fmt.Errorf("%s value: unknown policy %q", ev.Action, policy)
	}
	args, err := json.Marshal(jobArgs{ID: id, Missed: policy})
	if err != nil {
		return err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: "cron.update", WorkspaceID: ev.WorkspaceID, Args: args})
	return err
}

func (a *app) revealOwner(ctx context.Context, workspace, id string) (any, error) {
	a.mu.Lock()
	var owner string
	if j, ok := a.jobs[id]; ok && j.Workspace == workspace {
		owner = j.Owner
	}
	a.mu.Unlock()
	if owner == "" {
		return nil, fmt.Errorf("job %q has no owner session", id)
	}
	if err := agentcall.Reveal(ctx, a.host, workspace, owner); err != nil {
		return nil, err
	}
	return agentcall.Open(owner, 0), nil
}

func (a *app) renderWorkspace(ctx context.Context, workspace string) error {
	jobs := a.registry.InWorkspace(workspace)
	current, selected, err := automation.HelpSelection(ctx, a.host, workspace, jobs)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	open, err := automation.ReadOpen(ctx, a.host, workspace, selected)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	runs, err := a.runRows.View(ctx, a.host, workspace)
	if err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	titles, statuses := a.registry.Names(ctx, workspace, jobs)
	a.mu.Lock()
	d := a.drafts[workspace]
	a.mu.Unlock()
	now := a.now()
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(jobs, selected, now)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, mainView(current, jobs, titles, statuses, open, runs, d, now)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func (a *app) duplicate(ctx context.Context, workspace, jobID string) error {
	a.mu.Lock()
	j, ok := a.jobs[jobID]
	if !ok || j.Workspace != workspace {
		a.mu.Unlock()
		return fmt.Errorf("cron job %q not found", jobID)
	}
	c := j.Clone()
	c.Name += " copy"
	c.Enabled, c.Failures, c.LastError, c.History = false, 0, "", nil
	c.Paused = automation.Pause(nil, a.now(), "")
	for c.ID == j.ID || a.jobs[c.ID] != nil {
		c.ID = id.New(idPrefix)
	}
	err := a.put(ctx, c)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if err := a.host.StorageSet(ctx, automation.SelectedKey(workspace), c.ID); err != nil {
		return err
	}
	a.registry.Invalidate(workspace)
	a.registry.Flush(ctx)
	return nil
}

func (a *app) callCommand(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
	return automation.CallCommand(ctx, a.host, req)
}

func (a *app) listWorkspaces(ctx context.Context) ([]pluginsdk.Workspace, error) {
	return a.host.ListWorkspaces(ctx)
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

func wallNow() time.Time {
	return time.Now().Round(0)
}
