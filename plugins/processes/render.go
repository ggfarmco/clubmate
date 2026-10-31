package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
)

type group struct {
	ID        string
	ProjectID string
	Title     string
	Owner     string
}

type projectProcesses struct {
	sessions []group
	nodes    []pluginsdk.ProcessNode
}

func (a *app) tick(ctx context.Context, h *pluginsdk.Host) {
	a.pruneDeleted(time.Now())
	ids := a.presentIDs()
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
	byProject := splitByProject(sessions, nodes)
	for _, id := range ids {
		if !a.visible(id) {
			continue
		}
		p := byProject[id]
		logError("render "+id, a.show(ctx, h, id, p.sessions, p.nodes))
	}
}

func (a *app) renderLogged(ctx context.Context, h *pluginsdk.Host, id string) {
	logError("render "+id, a.render(ctx, h, id))
}

func (a *app) render(ctx context.Context, h *pluginsdk.Host, id string) error {
	_, err := a.state(ctx, h, id)
	if errors.Is(err, errProjectDeleted) {
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
	scope, totalKey, shown := "All", projectKey(id), nodes
	if i := slices.IndexFunc(sessions, func(s group) bool { return s.ID == snap.session }); i >= 0 {
		scope, totalKey = sessions[i].Title, sessionKey(snap.session)
		shown = slices.DeleteFunc(slices.Clone(nodes), func(n pluginsdk.ProcessNode) bool { return n.SessionID != snap.session })
	}
	a.forgetGone(st, &snap, nodes, shown)
	if err := a.showSidebar(ctx, h, id, st, snap.sidebar, sessions, snap.session); err != nil {
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

func (a *app) showSidebar(ctx context.Context, h *pluginsdk.Host, id string, st *projectState, sent *sidebarInputs, sessions []group, selected string) error {
	if sent != nil && sent.selected == selected && slices.Equal(sent.sessions, sessions) {
		return nil
	}
	if _, err := h.Render(ctx, id, sidebarSlot, sidebarView(sessions, selected)); err != nil {
		return err
	}
	a.mu.Lock()
	st.sidebar = &sidebarInputs{sessions: slices.Clone(sessions), selected: selected}
	a.mu.Unlock()
	return nil
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

func splitByProject(sessions []group, nodes []pluginsdk.ProcessNode) map[string]projectProcesses {
	out := make(map[string]projectProcesses)
	for _, s := range sessions {
		p := out[s.ProjectID]
		p.sessions = append(p.sessions, s)
		out[s.ProjectID] = p
	}
	projectOf := sessionProjects(sessions)
	for _, n := range nodes {
		id, ok := projectOf[n.SessionID]
		if !ok {
			continue
		}
		p := out[id]
		p.nodes = append(p.nodes, n)
		out[id] = p
	}
	return out
}

func sessionProjects(sessions []group) map[string]string {
	projectOf := make(map[string]string, len(sessions))
	for _, s := range sessions {
		projectOf[s.ID] = s.ProjectID
	}
	return projectOf
}
