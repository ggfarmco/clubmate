package main

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
)

type group struct {
	ID          string
	WorkspaceID string
	Title       string
	Owner       string
}

type workspaceProcesses struct {
	sessions []group
	nodes    []pluginsdk.ProcessNode
}

func (a *app) tick(ctx context.Context, h *pluginsdk.Host) {
	a.pruneDeleted(time.Now())
	ids := a.presentIDs()
	if len(ids) == 0 {
		return
	}
	steps := a.stepsDue(slices.ContainsFunc(ids, a.visible))
	if steps == 0 {
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
	core, err := scanCore(nodes)
	logError("core processes", err)
	a.setCore(core)
	a.renderMu.Lock()
	a.hist.recordSteps(steps, sessions, nodes, core, ids, time.Now())
	a.renderMu.Unlock()
	byWorkspace := splitByWorkspace(sessions, nodes)
	for _, id := range ids {
		if !a.visible(id) {
			continue
		}
		p := byWorkspace[id]
		withSessions, withNodes := withCore(id, p.sessions, p.nodes, core)
		logError("render "+id, a.show(ctx, h, id, withSessions, withNodes))
	}
}

func (a *app) stepsDue(shown bool) int {
	a.skipped++
	if !shown && a.skipped < hiddenSteps {
		return 0
	}
	steps := a.skipped
	a.skipped = 0
	return steps
}

func (a *app) coreNodes() []pluginsdk.ProcessNode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.core
}

func (a *app) setCore(core []pluginsdk.ProcessNode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.core, a.coreScanned = core, true
}

func (a *app) ensureCore(ctx context.Context, h *pluginsdk.Host) []pluginsdk.ProcessNode {
	a.mu.Lock()
	scanned := a.coreScanned
	a.mu.Unlock()
	if scanned {
		return a.coreNodes()
	}
	nodes, err := h.ProcessTree(ctx, "", "")
	if err != nil {
		logError("process tree", err)
		return nil
	}
	core, err := scanCore(nodes)
	logError("core processes", err)
	a.setCore(core)
	return core
}

func withCore(id string, sessions []group, nodes, core []pluginsdk.ProcessNode) ([]group, []pluginsdk.ProcessNode) {
	if len(core) == 0 {
		return sessions, nodes
	}
	return append(slices.Clip(sessions), coreGroup(id)), slices.Concat(nodes, core)
}

func (a *app) renderLogged(ctx context.Context, h *pluginsdk.Host, id string) {
	logError("render "+id, a.render(ctx, h, id))
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string) error {
	_, err := a.state(ctx, h, id)
	if errors.Is(err, errWorkspaceDeleted) {
		return nil
	}
	if err != nil {
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
	sessions, nodes = withCore(id, sessions, nodes, a.ensureCore(ctx, h))
	return a.show(ctx, h, id, sessions, nodes)
}

func (a *app) show(ctx context.Context, h *pluginsdk.Host, id string, sessions []group, nodes []pluginsdk.ProcessNode) error {
	cleared, err := a.draw(ctx, h, id, sessions, nodes)
	if cleared {
		err = errors.Join(err, h.StorageDelete(ctx, selectedKey(id)))
	}
	return err
}

func (a *app) draw(ctx context.Context, h *pluginsdk.Host, id string, sessions []group, nodes []pluginsdk.ProcessNode) (bool, error) {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	st, err := a.state(ctx, h, id)
	if errors.Is(err, errWorkspaceDeleted) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	snap := a.snapshot(st)
	cleared := false
	if snap.session != "" && !slices.ContainsFunc(sessions, func(s group) bool { return s.ID == snap.session }) {
		cleared = a.clearSession(st, snap.session)
		snap = a.snapshot(st)
	}
	scope, totalKey, shown, groups := "All", workspaceKey(id), nodes, sessions
	if i := slices.IndexFunc(sessions, func(s group) bool { return s.ID == snap.session }); i >= 0 {
		scope, totalKey, groups = sessions[i].Title, sessionKey(snap.session), sessions[i:i+1]
		shown = slices.DeleteFunc(slices.Clone(nodes), func(n pluginsdk.ProcessNode) bool { return n.SessionID != snap.session })
	}
	a.forgetGone(st, &snap, nodes, shown)
	if err := a.showSidebar(ctx, h, id, st, snap.sidebar, sessions, nodes, snap.session); err != nil {
		return cleared, err
	}
	view, depth := mainView(viewModel{
		nodes:     shown,
		groups:    groups,
		selected:  snap.pid,
		filter:    snap.filter,
		coreOpen:  snap.coreOpen,
		auto:      snap.auto,
		options:   snap.options,
		folded:    snap.folded,
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
	return cleared, err
}

func (a *app) showSidebar(ctx context.Context, h *pluginsdk.Host, id string, st *workspaceState, sent *sidebarInputs, sessions []group, nodes []pluginsdk.ProcessNode, selected string) error {
	cpu := sessionCPU(sessions, nodes)
	if sent != nil && sent.selected == selected && sent.total == len(nodes) && slices.Equal(sent.sessions, sessions) && maps.Equal(sent.cpu, cpu) {
		return nil
	}
	if _, err := h.Render(ctx, id, sidebarSlot, sidebarView(sessions, cpu, len(nodes), selected)); err != nil {
		return err
	}
	a.mu.Lock()
	st.sidebar = &sidebarInputs{sessions: slices.Clone(sessions), cpu: cpu, total: len(nodes), selected: selected}
	a.mu.Unlock()
	return nil
}

func (a *app) clearSession(st *workspaceState, stale string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st.session != stale {
		return false
	}
	st.session, st.pid = "", 0
	return true
}

func (a *app) forgetGone(st *workspaceState, snap *workspaceState, nodes, shown []pluginsdk.ProcessNode) {
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

func listGroups(ctx context.Context, h *pluginsdk.Host, workspaceID string) ([]group, error) {
	sessions, err := h.ListSessions(ctx, workspaceID, false)
	if err != nil {
		return nil, err
	}
	roots, err := h.ProcessRoots(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]group, 0, len(sessions)+len(roots))
	for _, s := range sessions {
		out = append(out, group{ID: s.ID, WorkspaceID: s.WorkspaceID, Title: s.Title, Owner: s.Owner})
	}
	for _, r := range roots {
		out = append(out, group{ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Label, Owner: r.Owner})
	}
	return out, nil
}

func splitByWorkspace(sessions []group, nodes []pluginsdk.ProcessNode) map[string]workspaceProcesses {
	out := make(map[string]workspaceProcesses)
	for _, s := range sessions {
		p := out[s.WorkspaceID]
		p.sessions = append(p.sessions, s)
		out[s.WorkspaceID] = p
	}
	workspaceOf := sessionWorkspaces(sessions)
	for _, n := range nodes {
		id, ok := workspaceOf[n.SessionID]
		if !ok {
			continue
		}
		p := out[id]
		p.nodes = append(p.nodes, n)
		out[id] = p
	}
	return out
}

func sessionWorkspaces(sessions []group) map[string]string {
	workspaceOf := make(map[string]string, len(sessions))
	for _, s := range sessions {
		workspaceOf[s.ID] = s.WorkspaceID
	}
	return workspaceOf
}
