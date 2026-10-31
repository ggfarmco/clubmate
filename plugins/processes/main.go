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

	"golang.org/x/sys/unix"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	sectionID       = "processes"
	sidebarSlot     = "processes.sidebar"
	mainSlot        = "processes.main"
	killedTopic     = "processes.killed"
	refreshInterval = time.Second
	autoKillEvery   = 10 * time.Second
	bytesPerKiB     = 1024
)

var (
	errUnknownAction  = errors.New("unknown action")
	errUnknownCommand = errors.New("unknown command")
	errUnknownColumn  = errors.New("unknown sort column")
	errNoProject      = errors.New("command needs a project")

	errProjectDeleted = errors.New("project deleted")
)

type killedEvent struct {
	ProjectID string `json:"projectId"`
	PIDs      []int  `json:"pids"`
}

type projectState struct {
	session   string
	pid       int
	auto      bool
	sort      sdui.Sort
	collapsed map[int]bool
	pidDepth  int
}

type app struct {
	renderMu sync.Mutex
	hist     *history
	memKiB   int64
	mu       sync.Mutex
	projects map[string]*projectState
	present  map[string]bool
	sections map[string]string
	deleted  map[string]bool
}

func main() {
	a := &app{hist: newHistory(), memKiB: memoryKiB(), projects: map[string]*projectState{}, present: map[string]bool{}, sections: map[string]string{}, deleted: map[string]bool{}}
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
	for _, id := range present {
		a.loadSection(ctx, h, id)
	}
	for _, p := range projects {
		logError("render "+p.ID, a.render(ctx, h, p.ID))
	}
	go a.refreshLoop(ctx, h)
	go a.autoKillLoop(ctx, h)
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case contract.TopicUIOpened, contract.TopicUIIdle:
		var p contract.UIPresence
		if !decode(ev, &p) {
			return
		}
		a.mu.Lock()
		if ev.Topic == contract.TopicUIIdle {
			delete(a.present, p.ProjectID)
		} else {
			a.present[p.ProjectID] = true
		}
		a.mu.Unlock()
		if ev.Topic == contract.TopicUIOpened {
			a.loadSection(ctx, h, p.ProjectID)
			logError("render "+p.ProjectID, a.render(ctx, h, p.ProjectID))
		}
	case contract.TopicUISection:
		var p contract.UISection
		if !decode(ev, &p) {
			return
		}
		a.setSection(p.ProjectID, p.Section)
		if p.Section == sectionID {
			logError("render "+p.ProjectID, a.render(ctx, h, p.ProjectID))
		}
	case contract.TopicProjectCreated:
		var p contract.ProjectChanged
		if decode(ev, &p) {
			logError("render "+p.Project.ID, a.render(ctx, h, p.Project.ID))
		}
	case contract.TopicProjectDeleted:
		var p contract.ProjectDeleted
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
		err = a.toggleCollapsed(ev, st)
	case "sort":
		err = a.setSort(ctx, h, ev, st)
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
	c := *st
	c.collapsed = maps.Clone(st.collapsed)
	return c
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
	pid, err := rowPID(ev)
	if err != nil || pid == 0 {
		return err
	}
	a.mu.Lock()
	st.pid = pid
	a.mu.Unlock()
	return nil
}

func (a *app) toggleCollapsed(ev pluginsdk.UIEvent, st *projectState) error {
	pid, err := rowPID(ev)
	if err != nil || pid == 0 {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ev.Action == "collapse" {
		st.collapsed[pid] = true
	} else {
		delete(st.collapsed, pid)
	}
	return nil
}

func (a *app) setSort(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, st *projectState) error {
	var s sdui.Sort
	if err := json.Unmarshal(ev.Value, &s); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if !validSort(s) {
		return fmt.Errorf("%w: %q", errUnknownColumn, s.Column)
	}
	a.mu.Lock()
	st.sort = s
	a.mu.Unlock()
	return h.StorageSet(ctx, sortKey(ev.ProjectID), s)
}

func rowPID(ev pluginsdk.UIEvent) (int, error) {
	var raw string
	if err := json.Unmarshal(ev.Value, &raw); err != nil {
		return 0, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if raw == moreID {
		return 0, nil
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return pid, nil
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
		a.tick(ctx, h)
	}
}

func (a *app) tick(ctx context.Context, h *pluginsdk.Host) {
	a.mu.Lock()
	ids := slices.Collect(maps.Keys(a.present))
	a.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	nodes, err := h.ProcessTree(ctx, "", "")
	if err != nil {
		logError("process tree", err)
		return
	}
	sessions, err := listGroups(ctx, h, "")
	if err != nil {
		logError("list groups", err)
		return
	}
	a.renderMu.Lock()
	a.hist.record(sessions, nodes, time.Now())
	a.renderMu.Unlock()
	projectOf := make(map[string]string, len(sessions))
	for _, s := range sessions {
		projectOf[s.ID] = s.ProjectID
	}
	for _, id := range ids {
		if !a.visible(id) {
			continue
		}
		own := slices.DeleteFunc(slices.Clone(sessions), func(s group) bool { return s.ProjectID != id })
		mine := slices.DeleteFunc(slices.Clone(nodes), func(n pluginsdk.ProcessNode) bool { return projectOf[n.SessionID] != id })
		logError("render "+id, a.show(ctx, h, id, own, mine))
	}
}

func (a *app) loadSection(ctx context.Context, h *pluginsdk.Host, id string) {
	section, err := h.SelectedSection(ctx, id)
	if err != nil {
		logError("section "+id, err)
		return
	}
	a.setSection(id, section)
}

func (a *app) setSection(id, section string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.deleted[id] {
		a.sections[id] = section
	}
}

func (a *app) visible(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	section := a.sections[id]
	return section == "" || section == sectionID
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
	delete(a.sections, id)
	a.deleted[id] = true
	a.mu.Unlock()
	logError("drop "+id, errors.Join(h.StorageDelete(ctx, selectedKey(id)), h.StorageDelete(ctx, autoKillKey(id)), h.StorageDelete(ctx, sortKey(id))))
}

func (a *app) state(ctx context.Context, h *pluginsdk.Host, id string) (*projectState, error) {
	a.mu.Lock()
	st, ok := a.projects[id]
	gone := a.deleted[id]
	a.mu.Unlock()
	if gone {
		return nil, errProjectDeleted
	}
	if ok {
		return st, nil
	}
	loaded := &projectState{collapsed: map[int]bool{}}
	if _, err := h.StorageGet(ctx, selectedKey(id), &loaded.session); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, autoKillKey(id), &loaded.auto); err != nil {
		return nil, err
	}
	if _, err := h.StorageGet(ctx, sortKey(id), &loaded.sort); err != nil {
		return nil, err
	}
	if !validSort(loaded.sort) {
		loaded.sort = defaultSort
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleted[id] {
		return nil, errProjectDeleted
	}
	if st, ok := a.projects[id]; ok {
		return st, nil
	}
	a.projects[id] = loaded
	return loaded, nil
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string) error {
	if _, err := a.state(ctx, h, id); errors.Is(err, errProjectDeleted) {
		return nil
	} else if err != nil {
		return err
	}
	nodes, err := h.ProcessTree(ctx, id, "")
	if err != nil {
		return err
	}
	sessions, err := listGroups(ctx, h, id)
	if err != nil {
		return err
	}
	return a.show(ctx, h, id, sessions, nodes)
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, id string, sessions []group, nodes []pluginsdk.ProcessNode) error {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	st, err := a.state(ctx, h, id)
	if errors.Is(err, errProjectDeleted) {
		return nil
	}
	if err != nil {
		return err
	}
	snap := a.snapshot(st)
	if snap.session != "" && !slices.ContainsFunc(sessions, func(s group) bool { return s.ID == snap.session }) {
		if err := a.clearSession(ctx, h, id, st, snap.session); err != nil {
			return err
		}
		snap = a.snapshot(st)
	}
	scope, totalKey := "All", projectKey(id)
	shown := nodes
	if i := slices.IndexFunc(sessions, func(s group) bool { return s.ID == snap.session }); i >= 0 {
		scope, totalKey = sessions[i].Title, sessionKey(snap.session)
		shown = slices.DeleteFunc(slices.Clone(nodes), func(n pluginsdk.ProcessNode) bool { return n.SessionID != snap.session })
	}
	a.forgetGone(st, &snap, nodes, shown)
	if _, err := h.Render(ctx, id, sidebarSlot, sidebarView(sessions, snap.session)); err != nil {
		return err
	}
	view, depth := mainView(viewModel{
		nodes:     shown,
		selected:  snap.pid,
		auto:      snap.auto,
		sort:      snap.sort,
		collapsed: snap.collapsed,
		scope:     scope,
		totalKey:  totalKey,
		hist:      a.hist,
		memKiB:    a.memKiB,
		pidDepth:  snap.pidDepth,
		now:       time.Now(),
	})
	a.mu.Lock()
	st.pidDepth = max(st.pidDepth, depth)
	a.mu.Unlock()
	_, err = h.Render(ctx, id, mainSlot, view)
	return err
}

func (a *app) forgetGone(st *projectState, snap *projectState, nodes, shown []pluginsdk.ProcessNode) {
	alive := make(map[int]bool, len(nodes))
	for _, n := range nodes {
		alive[n.Process.PID] = true
	}
	selected := slices.ContainsFunc(shown, func(n pluginsdk.ProcessNode) bool { return n.Process.PID == snap.pid })
	a.mu.Lock()
	defer a.mu.Unlock()
	if snap.pid != 0 && !selected {
		if st.pid == snap.pid {
			st.pid = 0
		}
		snap.pid = 0
	}
	for pid := range st.collapsed {
		if !alive[pid] {
			delete(st.collapsed, pid)
			delete(snap.collapsed, pid)
		}
	}
}

func listGroups(ctx context.Context, h *pluginsdk.Host, projectID string) ([]group, error) {
	sessions, err := h.ListSessions(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	roots, err := h.ProcessRoots(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]group, 0, len(sessions)+len(roots))
	for _, s := range sessions {
		out = append(out, group{ID: s.ID, ProjectID: s.ProjectID, Title: s.Title, Owner: s.Owner})
	}
	for _, r := range roots {
		out = append(out, group{ID: r.ID, ProjectID: r.ProjectID, Title: r.Label, Owner: r.Owner})
	}
	return out, nil
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

func sortKey(projectID string) string {
	return "sort/" + projectID
}

func memoryKiB() int64 {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		logError("hw.memsize", err)
		return 0
	}
	return int64(total / bytesPerKiB)
}
