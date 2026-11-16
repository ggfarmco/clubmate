package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
)

const (
	graphCommandPrefix = "agents.graph-"
	graphStartCommand  = "agents.graph-start"
	graphsSlotPrefix   = "agents.graphs."
	graphPrefix        = "graph:"
	gatePrefix         = "gate:"
	openGraph          = "graph"
	openGate           = "gate"
)

func (a *app) newGraphs(h *pluginsdk.Host) *graphs.Engine {
	return graphs.New(graphs.Deps{
		Host:     h,
		StateDir: filepath.Join(a.stateDir, "graphs"),
		Sessions: a.graphSessions,
		Needs:    a.setRunNeeds,
		ShowRun:  a.showRun,
		Changed:  a.graphsChanged,
	})
}

func (a *app) startGraphs(ctx context.Context, after int64) {
	if err := a.graphs.Start(ctx, after); err != nil {
		fmt.Fprintln(os.Stderr, "agents: start graphs:", err)
	}
}

func (a *app) graphSessions(_ context.Context, workspace string) ([]api.Session, error) {
	raw, err := json.Marshal(a.infos(workspace))
	if err != nil {
		return nil, err
	}
	var out []api.Session
	return out, json.Unmarshal(raw, &out)
}

func graphCommand(id string) bool {
	return strings.HasPrefix(id, graphCommandPrefix) && id != graphStartCommand
}

func openDockCommand(dock string) func(*app, context.Context, pluginsdk.CommandRun, struct{}) (any, error) {
	return func(a *app, ctx context.Context, cmd pluginsdk.CommandRun, _ struct{}) (any, error) {
		p := a.workspaceState(cmd.WorkspaceID)
		a.uiMu.Lock()
		p.dock = dock
		a.uiMu.Unlock()
		if err := a.render(ctx, cmd.WorkspaceID); err != nil {
			return nil, err
		}
		return nil, a.host.Focus(ctx, cmd.WorkspaceID, sectionID, sidebarSlot)
	}
}

func graphsSlot(slot string) bool {
	return strings.HasPrefix(slot, graphsSlotPrefix)
}

type graphState struct {
	runs     []api.RunRow
	dock     []api.DockRow
	defs     []graphs.Definition
	gates    []gateConfig
	landings map[string]int
}

func (a *app) graphState(ctx context.Context, workspace string, infos []sessionInfo) graphState {
	byID := make(map[string]sessionInfo, len(infos))
	var service []sessionInfo
	count := gateCount{}
	for _, info := range infos {
		byID[info.ID] = info
		if info.Service && !info.closed() {
			service = append(service, info)
		}
		if info.GateFor != nil && info.NeedsYou && !info.closed() {
			count.Asking++
		}
	}
	gates, _, err := a.workspaceGates(ctx, workspace)
	logError("land gates", err)
	count.Gated = len(gates)
	landings := map[string]int{}
	for _, g := range gates {
		for _, l := range a.gateLandings(workspace, g.ID) {
			landings[g.ID]++
			if l.State == gateStateNeedsMe {
				count.Asking++
			}
		}
	}
	defs := a.graphs.Definitions(ctx, workspace)
	return graphState{runs: runRows(a.graphs.Runs(workspace), byID), dock: dockRows(service, count, defs), defs: defs, gates: gates, landings: landings}
}

func (st graphState) plus(open string) sidebarPlus {
	plus := sidebarPlus{runs: st.runs, dock: st.dock, open: open}
	plus.gates = append(plus.gates, sdui.TreeItem{ID: dockGateHelp, Label: "How land gates work", Icon: "info.circle", Open: openGate})
	for _, g := range st.gates {
		plus.gates = append(plus.gates, sdui.TreeItem{ID: gatePrefix + g.ID, Label: gateName(g), Icon: gatesIcon, Detail: landingCount(st.landings[g.ID]), DetailTone: tonePlain, Open: openGate, Menu: gateRemoveMenu(gateName(g))})
	}
	plus.graphs = append(plus.graphs, sdui.TreeItem{ID: dockGraphHelp, Label: "How graphs work", Icon: "info.circle", Open: openGraph})
	for _, d := range st.defs {
		item := sdui.TreeItem{ID: graphPrefix + d.Name, Label: cmp.Or(d.Title, d.Name), Icon: "doc", Detail: strconv.Itoa(d.Nodes) + " nodes", Open: openGraph}
		switch {
		case d.Untrusted:
			item.Detail, item.DetailTone = "trust?", statusTones[statusWaiting]
		case d.Builtin:
			item.Detail = "built in"
		}
		plus.graphs = append(plus.graphs, item)
	}
	return plus
}

func (a *app) shownPage(workspace string) (string, bool) {
	p := a.workspaceState(workspace)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return p.selected, pageID(p.selected) && p.filter != filterOpen
}

func (a *app) deadPage(ctx context.Context, workspace, id string) bool {
	if run, ok := strings.CutPrefix(id, runPrefix); ok {
		running := a.graphs.Running(workspace)
		return running != nil && !slices.ContainsFunc(a.graphs.Runs(workspace), func(r graphs.RunSnapshot) bool { return r.ID == runOf(run) })
	}
	if gate, ok := strings.CutPrefix(id, gatePrefix); ok {
		all, err := a.gates(ctx)
		return err == nil && !slices.ContainsFunc(all, func(g gateConfig) bool { return g.ID == gate })
	}
	return false
}

func pageID(id string) bool {
	return strings.HasPrefix(id, runPrefix) || strings.HasPrefix(id, graphPrefix) || strings.HasPrefix(id, gatePrefix) || id == dockGraphHelp || id == dockGateHelp
}

func (a *app) selectPage(ctx context.Context, workspace, id string) error {
	if strings.HasPrefix(id, runPrefix) {
		id = runPrefix + runOf(id)
	}
	if run, ok := strings.CutPrefix(id, runPrefix); ok {
		for _, r := range a.graphs.Runs(workspace) {
			if r.ID == run && r.Status != graphRunning {
				if err := a.setCollapsed(ctx, workspace, map[string]bool{expandPrefix + id: true}); err != nil {
					return err
				}
			}
		}
	}
	p := a.workspaceState(workspace)
	a.uiMu.Lock()
	if !pageID(p.selected) {
		p.back = p.selected
	}
	a.uiMu.Unlock()
	return a.selectSession(ctx, workspace, id)
}

const (
	actBack       = "back"
	backNode      = "back"
	graphsMainOld = graphsSlotPrefix + "main"
)

func (a *app) showRun(ctx context.Context, workspace, run string) error {
	if err := a.selectPage(ctx, workspace, runPrefix+run); err != nil {
		return err
	}
	a.kick(workspace)
	return a.host.Focus(ctx, workspace, sectionID, mainSlot)
}

func (a *app) pageView(ctx context.Context, workspace, id, back string) sdui.Node {
	var page sdui.Node
	crumb := "Agents"
	switch {
	case strings.HasPrefix(id, gatePrefix), id == dockGateHelp:
		p := a.workspaceState(workspace)
		a.uiMu.Lock()
		picked := p.gatePick
		a.uiMu.Unlock()
		node, err := a.gatePage(ctx, workspace, strings.TrimPrefix(id, gatePrefix), picked)
		if err != nil {
			return sdui.Empty(err.Error())
		}
		page, crumb = node, crumb+" / Land gates"
	default:
		engineID := id
		if run, ok := strings.CutPrefix(id, runPrefix); ok {
			engineID = run
			for _, r := range a.graphs.Runs(workspace) {
				if r.ID == run {
					if branch := a.runBranch(ctx, workspace, r); branch != "" {
						crumb += " / " + branch
					}
					crumb += " / " + cmp.Or(r.Title, r.Graph)
					if r.Number > 0 {
						crumb += " · #" + strconv.Itoa(r.Number)
					}
				}
			}
		} else {
			crumb += " / Graphs"
		}
		if id == dockGraphHelp {
			engineID = automation.HelpID
		}
		page = a.graphs.Page(ctx, workspace, engineID)
	}
	bar := []sdui.Node{sdui.MutedText(crumb)}
	if back != "" {
		bar = append([]sdui.Node{sdui.WithShortcut(sdui.WithAction(sdui.Button(backNode, "Back to the session", "chevron.left"), actBack), "cmd+[")}, bar...)
	}
	if page.Type == "stack" && page.Props["direction"] == "vertical" {
		return sdui.VStack(append([]sdui.Node{sdui.HStack(bar...)}, page.Children...)...)
	}
	return sdui.VStack(sdui.HStack(bar...), page)
}

func (a *app) pageEvent(ctx context.Context, ev pluginsdk.UIEvent, selected string) (any, error) {
	if strings.HasPrefix(selected, gatePrefix) || selected == dockGateHelp {
		ev.Slot = gatesMainSlot
		if ev.Agent || a.session(ev.Caller) != nil {
			return nil, errUserAction
		}
		p := a.workspaceState(ev.WorkspaceID)
		a.uiMu.Lock()
		p.gate = strings.TrimPrefix(selected, gatePrefix)
		a.uiMu.Unlock()
		err := a.gatesAct(ctx, ev)
		a.kick(ev.WorkspaceID)
		return nil, err
	}
	ev.Slot = graphsMainOld
	return a.graphs.PageEvent(ctx, ev)
}

func (a *app) runBranch(ctx context.Context, workspace string, r graphs.RunSnapshot) string {
	worktree := r.Dir
	for _, n := range r.Nodes {
		if s := a.session(n.Session); n.Session != "" && s != nil && !s.snapshot().closed() {
			worktree = s.snapshot().Worktree
			break
		}
	}
	if worktree == "" {
		return ""
	}
	return cmp.Or(branchLabel(a.branches.name(ctx, workspace, worktree)), filepath.Base(worktree))
}

func (a *app) graphsChanged(workspace string) {
	a.kick(workspace)
	sigs := map[string]string{dockGraphs: strconv.Itoa(a.graphs.Refused(workspace))}
	for _, r := range a.graphs.Runs(workspace) {
		raw, err := json.Marshal(r)
		if err == nil {
			sigs[r.ID] = string(raw)
		}
	}
	a.runSigsMu.Lock()
	if a.runSigs == nil {
		a.runSigs = map[string]map[string]string{}
	}
	prev, seen := a.runSigs[workspace]
	a.runSigs[workspace] = sigs
	a.runSigsMu.Unlock()
	if !seen || a.host == nil {
		return
	}
	var changed []string
	for id, sig := range sigs {
		if prev[id] != sig {
			changed = append(changed, id)
		}
	}
	for id := range prev {
		if _, ok := sigs[id]; !ok {
			changed = append(changed, id)
		}
	}
	for _, id := range changed {
		_, err := a.host.Publish(context.Background(), api.TopicChanged, api.Changed{Workspace: workspace, Session: id, Kind: api.ChangeStatus})
		logError("publish run change", err)
	}
}
