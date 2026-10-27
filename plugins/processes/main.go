package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	sidebarSlot     = "processes.sidebar"
	mainSlot        = "processes.main"
	killedTopic     = "processes.killed"
	refreshInterval = 2 * time.Second
	autoKillEvery   = 10 * time.Second
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errNoProject      = errors.New("command needs a project")
)

type projectEvent struct {
	ProjectID string `json:"projectId"`
}

type projectCreated struct {
	Project pluginsdk.Project `json:"project"`
}

type projectDeleted struct {
	ID string `json:"id"`
}

type killedEvent struct {
	ProjectID string `json:"projectId"`
	PIDs      []int  `json:"pids"`
}

type projectState struct {
	session string
	pid     int
	auto    bool
}

type app struct {
	renderMu sync.Mutex
	mu       sync.Mutex
	projects map[string]*projectState
	present  map[string]bool
}

func main() {
	a := &app{projects: map[string]*projectState{}, present: map[string]bool{}}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.*", "project.*"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	for _, id := range present {
		a.present[id] = true
	}
	a.mu.Unlock()
	for _, p := range projects {
		logError("render "+p.ID, a.render(ctx, h, p.ID))
	}
	go a.refreshLoop(ctx, h)
	go a.autoKillLoop(ctx, h)
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "ui.opened", "ui.idle":
		var p projectEvent
		if !decode(ev, &p) {
			return
		}
		a.mu.Lock()
		if ev.Topic == "ui.idle" {
			delete(a.present, p.ProjectID)
		} else {
			a.present[p.ProjectID] = true
		}
		a.mu.Unlock()
		if ev.Topic == "ui.opened" {
			logError("render "+p.ProjectID, a.render(ctx, h, p.ProjectID))
		}
	case "project.created":
		var p projectCreated
		if decode(ev, &p) {
			logError("render "+p.Project.ID, a.render(ctx, h, p.Project.ID))
		}
	case "project.deleted":
		var p projectDeleted
		if decode(ev, &p) {
			a.drop(ctx, h, p.ID)
		}
	}
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	st, err := a.state(ctx, h, ev.ProjectID)
	if err != nil {
		return nil, err
	}
	snap := a.snapshot(st)
	switch ev.Action {
	case "expand", "collapse":
		return nil, nil
	case killAction:
		err = killSelected(ctx, h, ev.ProjectID, snap)
	case orphanKill:
		_, err = killOrphans(ctx, h, ev.ProjectID, snap.session)
	case "select":
		err = a.selectSession(ctx, h, ev, st)
	case selectPID:
		err = a.selectProcess(ev, st)
	case autoKill:
		err = a.toggleAuto(ctx, h, ev, st)
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	if err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h, ev.ProjectID)
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID != "processes.kill-orphans" {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
	}
	if cmd.ProjectID == "" {
		return nil, errNoProject
	}
	st, err := a.state(ctx, h, cmd.ProjectID)
	if err != nil {
		return nil, err
	}
	if _, err := killOrphans(ctx, h, cmd.ProjectID, a.snapshot(st).session); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, h, cmd.ProjectID)
}

func (a *app) snapshot(st *projectState) projectState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *st
}

func (a *app) selectSession(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *projectState) error {
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return fmt.Errorf("select value: %w", err)
	}
	if id == allID {
		id = ""
	}
	a.mu.Lock()
	st.session, st.pid = id, 0
	a.mu.Unlock()
	if id == "" {
		return h.StorageDelete(ctx, selectedKey(ev.ProjectID))
	}
	return h.StorageSet(ctx, selectedKey(ev.ProjectID), id)
}

func (a *app) clearSession(ctx context.Context, h *pluginsdk.Host, projectID string, st *projectState, stale string) error {
	a.mu.Lock()
	cleared := st.session == stale
	if cleared {
		st.session, st.pid = "", 0
	}
	a.mu.Unlock()
	if !cleared {
		return nil
	}
	return h.StorageDelete(ctx, selectedKey(projectID))
}

func (a *app) selectProcess(ev pluginsdk.UIEvent, st *projectState) error {
	var raw string
	if err := json.Unmarshal(ev.Value, &raw); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	a.mu.Lock()
	st.pid = pid
	a.mu.Unlock()
	return nil
}

func killSelected(ctx context.Context, h *pluginsdk.Host, projectID string, st projectState) error {
	if st.pid == 0 {
		return nil
	}
	nodes, err := h.ProcessTree(ctx, projectID, st.session)
	if err != nil {
		return err
	}
	pids := subtree(nodes, st.pid)
	if len(pids) == 0 {
		return nil
	}
	_, err = h.KillProcesses(ctx, pluginsdk.KillRequest{PIDs: pids, ProjectID: projectID, SessionID: st.session})
	return err
}

func killOrphans(ctx context.Context, h *pluginsdk.Host, projectID, sessionID string) ([]int, error) {
	return h.KillProcesses(ctx, pluginsdk.KillRequest{ProjectID: projectID, SessionID: sessionID, OrphansOnly: true})
}

func (a *app) toggleAuto(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *projectState) error {
	var on bool
	if err := json.Unmarshal(ev.Value, &on); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	a.mu.Lock()
	st.auto = on
	a.mu.Unlock()
	if !on {
		return h.StorageDelete(ctx, autoKillKey(ev.ProjectID))
	}
	return h.StorageSet(ctx, autoKillKey(ev.ProjectID), true)
}

func sweep(ctx context.Context, h *pluginsdk.Host, projectID string) {
	killed, err := killOrphans(ctx, h, projectID, "")
	if err != nil {
		logError("auto-kill "+projectID, err)
		return
	}
	if len(killed) == 0 {
		return
	}
	_, err = h.Publish(ctx, killedTopic, killedEvent{ProjectID: projectID, PIDs: killed})
	logError("publish "+killedTopic, err)
}

func (a *app) refreshLoop(ctx context.Context, h *pluginsdk.Host) {
	tick := time.NewTicker(refreshInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a.mu.Lock()
		ids := slices.Collect(maps.Keys(a.present))
		a.mu.Unlock()
		for _, id := range ids {
			logError("render "+id, a.render(ctx, h, id))
		}
	}
}

func (a *app) autoKillLoop(ctx context.Context, h *pluginsdk.Host) {
	tick := time.NewTicker(autoKillEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a.mu.Lock()
		var enabled []string
		for id, st := range a.projects {
			if st.auto {
				enabled = append(enabled, id)
			}
		}
		a.mu.Unlock()
		for _, id := range enabled {
			sweep(ctx, h, id)
		}
	}
}

func (a *app) drop(ctx context.Context, h *pluginsdk.Host, id string) {
	a.mu.Lock()
	delete(a.projects, id)
	delete(a.present, id)
	a.mu.Unlock()
	logError("drop "+id, errors.Join(h.StorageDelete(ctx, selectedKey(id)), h.StorageDelete(ctx, autoKillKey(id))))
}

func (a *app) state(ctx context.Context, h *pluginsdk.Host, id string) (*projectState, error) {
	a.mu.Lock()
	st, ok := a.projects[id]
	a.mu.Unlock()
	if ok {
		return st, nil
	}
	loaded := &projectState{}
	if _, err := h.StorageGet(ctx, selectedKey(id), &loaded.session); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, autoKillKey(id), &loaded.auto); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.projects[id]; ok {
		return st, nil
	}
	a.projects[id] = loaded
	return loaded, nil
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string) error {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	st, err := a.state(ctx, h, id)
	if err != nil {
		return err
	}
	sessions, err := h.ListSessions(ctx, id, false)
	if err != nil {
		return err
	}
	snap := a.snapshot(st)
	if snap.session != "" && !slices.ContainsFunc(sessions, func(s pluginsdk.Session) bool { return s.ID == snap.session }) {
		if err := a.clearSession(ctx, h, id, st, snap.session); err != nil {
			return err
		}
		snap = a.snapshot(st)
	}
	nodes, err := h.ProcessTree(ctx, id, snap.session)
	if err != nil {
		return err
	}
	if snap.pid != 0 && !slices.ContainsFunc(nodes, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == snap.pid }) {
		a.mu.Lock()
		if st.pid == snap.pid {
			st.pid = 0
		}
		a.mu.Unlock()
		snap.pid = 0
	}
	rows := make([]sessionRow, 0, len(sessions))
	for _, s := range sessions {
		rows = append(rows, sessionRow{ID: s.ID, Title: s.Title, Owner: s.Owner})
	}
	if _, err := h.Render(ctx, id, sidebarSlot, sidebarView(rows, snap.session)); err != nil {
		return err
	}
	_, err = h.Render(ctx, id, mainSlot, mainView(nodes, snap.pid, snap.auto))
	return err
}

func subtree(nodes []pluginsdk.ProcessNode, root int) []int {
	children := make(map[int][]int, len(nodes))
	found := false
	for _, n := range nodes {
		children[n.Process.PPID] = append(children[n.Process.PPID], n.Process.PID)
		found = found || n.Process.PID == root
	}
	if !found {
		return nil
	}
	pids := []int{root}
	for i := 0; i < len(pids); i++ {
		pids = append(pids, children[pids[i]]...)
	}
	return pids
}

func decode(ev pluginsdk.Event, into any) bool {
	if err := json.Unmarshal(ev.Payload, into); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "processes: %s: %v\n", what, err)
	}
}

func selectedKey(projectID string) string {
	return "selected/" + projectID
}

func autoKillKey(projectID string) string {
	return "autokill/" + projectID
}
