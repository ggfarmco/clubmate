package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	reasonBudget    = "turn budget of %d reached"
	reasonCaller    = "the session that ran it is closed"
	reasonStopped   = "stopped"
	reasonKickoff   = "start failed: %v"
	reasonEnd       = "%s reported %s"
	callTimeout     = 30 * time.Second
	agentsTopics    = "agents.*"
	workspaceTopics = "workspace.*"
	outcomeTopics   = eventPrefix + graphTopic + "*"
	selectedPrefix  = "selected/"
	graphSuffix     = "/graph"
	actOpenMove     = "openMove"
	actOpenEdge     = "openEdge"
	triggersSection = "triggers"
)

var errNotYours = errors.New("this run was not started by this agent session or its child sessions")

type app struct {
	mu          sync.Mutex
	renderMu    sync.Mutex
	runs        map[string]*Run
	dirty       map[string]bool
	drafts      map[string]draft
	starting    map[string]string
	collapsed   map[string]map[string]bool
	lastSeq     atomic.Int64
	subs        map[string]string
	store       automation.Storage[*Run]
	host        *pluginsdk.Host
	ready       chan struct{}
	now         func() time.Time
	runCommand  func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error)
	sessions    func(ctx context.Context, workspace string) ([]api.Session, error)
	workspace   func(ctx context.Context, workspace string) (pluginsdk.Workspace, error)
	workspaces  func(ctx context.Context) ([]pluginsdk.Workspace, error)
	render      func(ctx context.Context, workspace string) error
	choose      func(ctx context.Context, workspace, id string) error
	revealTurn  func(ctx context.Context, workspace, session string, turn int) error
	proc        procRunner
	spawn       func(f func())
	pending     map[string]pendingStep
	callStep    func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error)
	kvGet       func(ctx context.Context, key string, out any) (bool, error)
	kvSet       func(ctx context.Context, key string, v any) error
	cancels     map[string]context.CancelFunc
	roots       func(ctx context.Context, workspace string) ([]pluginsdk.ProcessRoot, error)
	kill        func(ctx context.Context, req pluginsdk.KillRequest) error
	register    func(ctx context.Context, workspace string, pid int, label string)
	unregister  func(ctx context.Context, pid int)
	home        string
	managedPath string
	publish     func(ctx context.Context, topic string, payload any) error
	selectTrig  func(ctx context.Context, workspace, trigger string) error
}

type draft struct {
	graph string
	input string
	err   string
}

type outcomePayload struct {
	Workspace string `json:"workspace"`
	By        string `json:"by"`
	Outcome   string `json:"outcome"`
}

func main() {
	a := newApp()
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed})
}

func newApp() *app {
	a := &app{
		runs:      map[string]*Run{},
		dirty:     map[string]bool{},
		drafts:    map[string]draft{},
		starting:  map[string]string{},
		collapsed: map[string]map[string]bool{},
		subs:      map[string]string{},
		ready:     make(chan struct{}),
		now:       time.Now,
	}
	a.runCommand = a.callCommand
	a.sessions = a.agentSessions
	a.workspace = a.getWorkspace
	a.workspaces = a.listWorkspaces
	a.render = a.renderWorkspace
	a.choose = a.storeSelected
	a.revealTurn = a.revealSession
	a.proc = shellRunner{grace: 5 * time.Second}
	a.pending = map[string]pendingStep{}
	a.callStep = func(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
		return automation.CallCommand(ctx, a.host, req)
	}
	a.kvGet = a.storageGet
	a.kvSet = a.storageSet
	a.cancels = map[string]context.CancelFunc{}
	a.roots = a.processRoots
	a.kill = a.killProcesses
	a.register = a.registerProcess
	a.unregister = a.unregisterProcess
	a.home, _ = os.UserHomeDir()
	a.managedPath = managedPath
	a.spawn = func(f func()) { go f() }
	a.publish = a.publishEvent
	a.selectTrig = a.selectTrigger
	return a
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	st := &automation.Store[Run, *Run]{Host: h, Prefix: runPrefix, Noun: "graph run", Log: logError}
	runs, err := st.Load(ctx)
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
	for _, pattern := range []string{agentsTopics, workspaceTopics, outcomeTopics} {
		if err := a.subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	a.recoverSteps(ctx)
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
	case api.TopicTurn:
		var p api.Turn
		if decode(ev, &p) {
			a.countTurn(ctx, p, ev.Source)
		}
	case api.TopicChanged:
		var p api.Changed
		if decode(ev, &p) {
			a.sessionChanged(ctx, p)
		}
	case api.TopicReady:
		a.renderAll(ctx)
	case automation.TopicWorkspaceCreated:
		var p contract.WorkspaceChanged
		if decode(ev, &p) {
			a.invalidate(p.Workspace)
			a.flush(ctx)
		}
	case automation.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if decode(ev, &p) {
			a.dropWorkspace(ctx, p.Workspace)
		}
	default:
		var p outcomePayload
		if run, node, ok := parseOutcomeTopic(ev.Topic); ok && decode(ev, &p) {
			a.reportOutcome(ctx, run, node, p, ev.Source)
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
	if run, ok := strings.CutSuffix(ev.NodeID, graphSuffix); ok && ev.Slot == mainSlot {
		if strings.HasPrefix(run, graphPrefix) {
			return nil, nil
		}
		return nil, a.openGraph(ctx, ev, run)
	}
	var id string
	switch ev.Slot {
	case mainSlot:
		target, ok := automation.ActionTarget(ev.NodeID, ev.Action)
		if !ok {
			return nil, nil
		}
		id = target
	case sidebarSlot:
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return nil, fmt.Errorf("%s value: %w", ev.Action, err)
		}
	default:
		return nil, nil
	}
	switch {
	case ev.Action == "expand" || ev.Action == "collapse":
		a.fold(ev.WorkspaceID, id, ev.Action == "collapse")
		a.invalidate(ev.WorkspaceID)
		a.flush(ctx)
		return nil, nil
	case strings.HasPrefix(id, groupPrefix):
		return nil, nil
	case ev.Action == actRun:
		return nil, a.runFromSection(ctx, ev, strings.TrimPrefix(id, graphPrefix))
	}
	cmd := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID}
	switch ev.Action {
	case actOpen:
		return nil, a.openAttention(ctx, ev.WorkspaceID, id)
	case "select":
		a.mu.Lock()
		delete(a.drafts, ev.WorkspaceID)
		a.mu.Unlock()
		return nil, a.choose(ctx, ev.WorkspaceID, id)
	case actStop:
		cmd.ID = "graphs.stop"
	case actRemove:
		cmd.ID = "graphs.delete"
	default:
		return nil, fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	args, err := json.Marshal(automation.IDArgs{ID: id})
	if err != nil {
		return nil, err
	}
	cmd.Args = args
	_, err = a.command(ctx, nil, cmd)
	return nil, err
}

func (a *app) runFromSection(ctx context.Context, ev pluginsdk.UIEvent, graph string) error {
	var values map[string]string
	if err := json.Unmarshal(ev.Value, &values); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	a.mu.Lock()
	if a.starting[ev.WorkspaceID] != "" {
		a.mu.Unlock()
		return nil
	}
	a.starting[ev.WorkspaceID] = graph
	a.touch(ev.WorkspaceID)
	a.mu.Unlock()
	a.flush(ctx)
	ctx = context.WithoutCancel(ctx)
	go func() {
		logError(a.startGraph(ctx, ev.WorkspaceID, graph, strings.TrimSpace(values[fieldInput])))
	}()
	return nil
}

func (a *app) startGraph(ctx context.Context, workspace, graph, input string) error {
	args, err := json.Marshal(runArgs{Graph: graph, Input: input})
	if err != nil {
		return err
	}
	out, err := a.command(ctx, nil, pluginsdk.CommandRun{ID: "graphs.run", WorkspaceID: workspace, Args: args})
	a.mu.Lock()
	delete(a.starting, workspace)
	if err != nil {
		a.drafts[workspace] = draft{graph: graph, input: input, err: automation.Capitalize(err.Error()) + "."}
	} else {
		delete(a.drafts, workspace)
	}
	a.touch(workspace)
	a.mu.Unlock()
	if err != nil {
		a.flush(ctx)
		return nil
	}
	return a.choose(ctx, workspace, out.(*Run).ID)
}

func (a *app) storeSelected(ctx context.Context, workspace, id string) error {
	if err := a.host.StorageSet(ctx, selectedPrefix+workspace, id); err != nil {
		return err
	}
	a.invalidate(workspace)
	a.flush(ctx)
	return nil
}

func (a *app) fold(workspace, id string, collapsed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.collapsed[workspace] == nil {
		a.collapsed[workspace] = map[string]bool{}
	}
	a.collapsed[workspace][id] = collapsed
}

func (a *app) openAttention(ctx context.Context, workspace, id string) error {
	a.mu.Lock()
	r := a.runs[id]
	if r != nil && r.Workspace == workspace {
		r = cloneRun(r)
	} else {
		r = nil
	}
	a.mu.Unlock()
	if r == nil {
		return unknownRun(id)
	}
	n, _ := attentionNode(r, a.sessionMap(ctx, workspace))
	if n == nil {
		return fmt.Errorf("graph run %q has no open session", id)
	}
	return a.revealTurn(ctx, workspace, n.Session, 0)
}

func (a *app) openGraph(ctx context.Context, ev pluginsdk.UIEvent, id string) error {
	r, err := a.lookup(ev.WorkspaceID, id)
	if err != nil {
		return err
	}
	var node string
	turn := 0
	switch ev.Action {
	case actOpen:
		if err := json.Unmarshal(ev.Value, &node); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actOpenMove:
		var i int
		if err := json.Unmarshal(ev.Value, &i); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		if i < 0 || i >= len(r.Moves) {
			return fmt.Errorf("graph run %q has no move %d", id, i)
		}
		node, turn = r.Moves[i].Node, r.Moves[i].Turn
	case actOpenEdge:
		var trigger string
		if err := json.Unmarshal(ev.Value, &trigger); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		if r.Status != statusRunning {
			return fmt.Errorf("graph run %q is %s: its triggers are gone", id, r.Status)
		}
		if trigger == "" || !slices.ContainsFunc(r.Edges, func(e RunEdge) bool { return e.Trigger == trigger }) {
			return fmt.Errorf("graph run %q has no trigger %q", id, trigger)
		}
		return a.selectTrig(ctx, ev.WorkspaceID, trigger)
	default:
		return fmt.Errorf("%w: action %q", agentcall.ErrUnknownCommand, ev.Action)
	}
	n := r.node(node)
	if n != nil && n.Step {
		return nil
	}
	if n == nil || n.Session == "" {
		return fmt.Errorf("graph run %q has no session for node %q", id, node)
	}
	return a.revealTurn(ctx, ev.WorkspaceID, n.Session, turn)
}

func (a *app) selectTrigger(ctx context.Context, workspace, trigger string) error {
	value, err := json.Marshal(trigger)
	if err != nil {
		return err
	}
	if _, err := a.host.UIEvent(ctx, pluginsdk.UIEvent{WorkspaceID: workspace, Slot: triggersSection + ".sidebar", NodeID: triggersSection, Action: "select", Value: value}); err != nil {
		return fmt.Errorf("select trigger %s: %w", trigger, err)
	}
	return a.host.Focus(ctx, workspace, triggersSection, triggersSection+".main")
}

func (a *app) revealSession(ctx context.Context, workspace, session string, turn int) error {
	if turn == 0 {
		return agentcall.Reveal(ctx, a.host, workspace, session)
	}
	return agentcall.RevealTurn(ctx, a.host, workspace, session, turn)
}

func (a *app) countTurn(ctx context.Context, p api.Turn, source string) {
	if source != agentsSource {
		return
	}
	a.mu.Lock()
	var spent []*Run
	var owed []delivery
	for _, r := range a.runs {
		n := r.nodeBySession(p.Session)
		if r.Workspace != p.Workspace || n == nil {
			continue
		}
		if r.Status != statusRunning {
			if turnOver(p) && r.closeMove(n.ID, p.Turn) >= 0 {
				a.touch(r.Workspace)
				logError(a.store.Save(ctx, r))
			}
			continue
		}
		before := len(r.Moves)
		r.Turns++
		r.endTurn(p, a.now())
		owed = append(owed, r.owedSince(before, ownedCause{Reply: p.Reply})...)
		a.touch(r.Workspace)
		if r.Turns >= r.MaxTurns {
			r.stop(fmt.Sprintf(reasonBudget, r.MaxTurns), a.now())
			spent = append(spent, cloneRun(r))
		}
		logError(a.store.Save(ctx, r))
	}
	a.mu.Unlock()
	for _, r := range spent {
		a.finish(ctx, r)
	}
	for _, d := range owed {
		a.deliver(ctx, d)
	}
	a.flush(ctx)
}

func (a *app) reportOutcome(ctx context.Context, runID, nodeID string, p outcomePayload, source string) {
	if source != triggersSource {
		return
	}
	a.mu.Lock()
	r := a.runs[runID]
	if r == nil || r.Workspace != p.Workspace || r.Status != statusRunning || r.Spec == nil {
		a.mu.Unlock()
		return
	}
	n, spec := r.node(nodeID), r.Spec.node(nodeID)
	if n == nil || spec == nil || spec.step() || p.By == "" || n.Session != p.By {
		a.mu.Unlock()
		return
	}
	before := len(r.Moves)
	r.outcome(nodeID, p.Outcome, a.now())
	owed := r.owedSince(before, ownedCause{Outcome: p.Outcome})
	var ended *Run
	if slices.Contains(spec.End, p.Outcome) {
		r.stop(fmt.Sprintf(reasonEnd, nodeID, p.Outcome), a.now())
		ended = cloneRun(r)
	}
	a.touch(r.Workspace)
	logError(a.store.Save(ctx, r))
	a.mu.Unlock()
	if ended != nil {
		a.finish(ctx, ended)
	}
	for _, d := range owed {
		a.deliver(ctx, d)
	}
	a.flush(ctx)
}

func (a *app) sessionChanged(ctx context.Context, p api.Changed) {
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
	if p.Kind == api.ChangeCreated {
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
			logError(a.store.Save(ctx, r))
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

func (a *app) checkCaller(ctx context.Context, p api.Changed) {
	if p.Kind != api.ChangeDeleted {
		sessions, err := a.sessions(ctx, p.Workspace)
		if err != nil {
			logError(err)
			return
		}
		if s, ok := agentcall.Find(sessions, p.Session); ok && !s.Closed() {
			return
		}
	}
	a.mu.Lock()
	var stopped []string
	for _, r := range a.runs {
		if r.Workspace == p.Workspace && r.Status == statusRunning && r.Caller == p.Session {
			r.stop(reasonCaller, a.now())
			logError(a.store.Save(ctx, r))
			stopped = append(stopped, r.ID)
		}
	}
	a.mu.Unlock()
	for _, id := range stopped {
		a.cancelSteps(ctx, id)
	}
}

func (a *app) dropWorkspace(ctx context.Context, workspace string) {
	workspaces, err := a.workspaces(ctx)
	if err != nil {
		logError(fmt.Errorf("check deleted workspace %s: %w", workspace, err))
		return
	}
	if slices.ContainsFunc(workspaces, func(p pluginsdk.Workspace) bool { return p.ID == workspace }) {
		return
	}
	a.mu.Lock()
	var dropped []string
	for id, r := range a.runs {
		if r.Workspace == workspace {
			delete(a.runs, id)
			logError(a.store.Remove(ctx, r))
			dropped = append(dropped, id)
			removeOutput(id)
		}
	}
	delete(a.dirty, workspace)
	a.mu.Unlock()
	for _, id := range dropped {
		a.cancelSteps(ctx, id)
	}
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
	p, err := a.workspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return p.Roots(), nil
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
	sessions, err := a.sessions(ctx, cmd.WorkspaceID)
	if err != nil {
		return err
	}
	if agentcall.Descends(r.Caller, cmd.Caller, agentcall.Parents(sessions)) {
		return nil
	}
	return errNotYours
}

func (a *app) renderAll(ctx context.Context) {
	workspaces, err := a.workspaces(ctx)
	if err != nil {
		logError(fmt.Errorf("render graphs sections: %w", err))
		return
	}
	ids := make([]string, 0, len(workspaces))
	for _, p := range workspaces {
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
	dirs, err := a.graphDirs(ctx, workspace, false, "")
	logError(err)
	graphs := listGraphs(dirs)
	runs := a.workspaceRuns(workspace)
	var statuses map[string]api.Session
	if len(runs) > 0 {
		statuses = a.sessionMap(ctx, workspace)
	}
	selected = pickSelected(selected, graphs, runs)
	a.mu.Lock()
	d, starting, collapsed := a.drafts[workspace], a.starting[workspace], maps.Clone(a.collapsed[workspace])
	a.mu.Unlock()
	if _, err := a.host.Render(ctx, workspace, sidebarSlot, sidebarView(graphs, runs, selected, statuses, collapsed)); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	main := emptyView()
	if i := slices.IndexFunc(runs, func(r *Run) bool { return r.ID == selected }); i >= 0 {
		main = mainView(runs[i], statuses)
	} else if i := slices.IndexFunc(graphs, func(g graphFile) bool { return graphPrefix+g.Name == selected }); i >= 0 {
		g := graphs[i]
		var spec *Graph
		if g.Error == "" {
			spec, _, err = loadGraph(dirs, g.Name)
			logError(err)
		}
		main = graphView(g, spec, d, starting)
	}
	if _, err := a.host.Render(ctx, workspace, mainSlot, main); err != nil {
		return fmt.Errorf("render %s: %w", workspace, err)
	}
	return nil
}

func pickSelected(selected string, graphs []graphFile, runs []*Run) string {
	if slices.ContainsFunc(runs, func(r *Run) bool { return r.ID == selected }) || slices.ContainsFunc(graphs, func(g graphFile) bool { return graphPrefix+g.Name == selected }) {
		return selected
	}
	if len(runs) > 0 {
		return runs[0].ID
	}
	if i := slices.IndexFunc(graphs, func(g graphFile) bool { return g.Error == "" }); i >= 0 {
		return graphPrefix + graphs[i].Name
	}
	return ""
}

func (a *app) sessionMap(ctx context.Context, workspace string) map[string]api.Session {
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		logError(err)
		return nil
	}
	m := make(map[string]api.Session, len(sessions))
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

func (a *app) callCommand(ctx context.Context, req automation.CommandRequest) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return automation.CallCommand(ctx, a.host, req)
}

func (a *app) agentSessions(ctx context.Context, workspace string) ([]api.Session, error) {
	return agentcall.Sessions(ctx, a.host, workspace)
}

func (a *app) getWorkspace(ctx context.Context, workspace string) (pluginsdk.Workspace, error) {
	return a.host.GetWorkspace(ctx, workspace)
}

func (a *app) listWorkspaces(ctx context.Context) ([]pluginsdk.Workspace, error) {
	return a.host.ListWorkspaces(ctx)
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
