package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

type runArgs struct {
	Graph string `json:"graph"`
	Input string `json:"input"`
}

type listResult struct {
	Graphs []graphFile `json:"graphs"`
	Runs   []runRow    `json:"runs"`
}

type runRow struct {
	ID        string `json:"id"`
	Graph     string `json:"graph"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Turns     int    `json:"turns"`
	MaxTurns  int    `json:"maxTurns"`
	StartedAt string `json:"startedAt"`
}

type shownRun struct {
	*Run
	Nodes []shownNode `json:"nodes"`
}

type shownNode struct {
	RunNode
	Title      string       `json:"title,omitempty"`
	Status     string       `json:"status"`
	Command    string       `json:"command,omitempty"`
	Outcome    string       `json:"outcome,omitempty"`
	DurationMs int64        `json:"durationMs,omitempty"`
	StartedAt  *time.Time   `json:"startedAt,omitempty"`
	Last       *StepMove    `json:"last,omitempty"`
	Pending    *pendingInfo `json:"pending,omitempty"`
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer a.flush(ctx)
	switch cmd.ID {
	case "graphs.list":
		return a.list(ctx, cmd)
	case "graphs.run":
		return a.start(ctx, cmd)
	case "graphs.show":
		return a.show(ctx, cmd)
	case "graphs.stop":
		return a.stopRun(ctx, cmd, false)
	case "graphs.delete":
		_, err := a.stopRun(ctx, cmd, true)
		return nil, err
	case "graphs.reveal":
		return nil, a.reveal(ctx, cmd)
	case "graphs.new":
		return nil, a.askAgent(ctx, cmd.WorkspaceID, "")
	case "graphs.approve":
		return nil, a.approveStep(ctx, cmd)
	case "graphs.continue":
		return nil, a.continueEdge(ctx, cmd)
	case "graphs.trust":
		return nil, a.trustGraph(ctx, cmd)
	}
	return nil, fmt.Errorf("%w: %q", agentcall.ErrUnknownCommand, cmd.ID)
}

func (a *app) list(ctx context.Context, cmd pluginsdk.CommandRun) (listResult, error) {
	dirs, err := a.graphDirs(ctx, cmd.WorkspaceID, cmd.Agent, cmd.Caller)
	if err != nil {
		return listResult{}, err
	}
	res := listResult{Graphs: listGraphs(dirs, a.home)}
	roots := map[string]func() string{}
	for i := range res.Graphs {
		f := &res.Graphs[i]
		if f.Error != "" {
			continue
		}
		if g, _, err := loadGraph(dirs, a.home, f.Name); err == nil {
			dir := trustDir(dirs, f.Name)
			root, ok := roots[dir]
			if !ok {
				root = rootOf(ctx, dir)
				roots[dir] = root
			}
			f.Hash = stepsHash(g)
			f.Untrusted, f.Changed, err = a.trustState(ctx, root, g)
			logError(err)
			if !f.Untrusted {
				continue
			}
			if last, ok, err := a.lastTrusted(ctx, root(), f.Name); ok {
				f.Trusted = &last
			} else {
				logError(err)
			}
		}
	}
	for _, r := range a.workspaceRuns(cmd.WorkspaceID) {
		res.Runs = append(res.Runs, runRow{
			ID:        r.ID,
			Graph:     r.Graph,
			Title:     r.Title,
			Status:    r.Status,
			Reason:    r.Reason,
			Turns:     r.Turns,
			MaxTurns:  r.MaxTurns,
			StartedAt: r.StartedAt.UTC().Format(timeLayout),
		})
	}
	res.Graphs, res.Runs = nonNil(res.Graphs), nonNil(res.Runs)
	return res, nil
}

func (a *app) start(ctx context.Context, cmd pluginsdk.CommandRun) (*Run, error) {
	var args runArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return nil, err
	}
	caller := ""
	if cmd.Agent {
		caller = cmd.Caller
	}
	dirs, err := a.graphDirs(ctx, cmd.WorkspaceID, cmd.Agent, caller)
	if err != nil {
		return nil, err
	}
	g, from, err := loadGraph(dirs, a.home, args.Graph)
	if err != nil {
		return nil, err
	}
	switch untrusted, _, err := a.trustState(ctx, rootOf(ctx, trustDir(dirs, args.Graph)), g); {
	case err != nil:
		return nil, err
	case untrusted:
		return nil, fmt.Errorf(errUntrusted, args.Graph)
	}
	r, err := newRun(g, cmd.WorkspaceID, args.Input, cmd.Agent, caller, a.now())
	if err != nil {
		return nil, err
	}
	r.Builtin, r.User = from == fromBuiltin, from == fromUser
	r.Dir = graphDir(dirs, args.Graph)
	r.Starts = a.startCommits(ctx, cmd.WorkspaceID, append(dirs, r.Dir))
	x := &runner{call: a.runCommand, run: r}
	if err := x.start(ctx, g); err != nil {
		cleanup := context.WithoutCancel(ctx)
		x.unwind(cleanup)
		x.closeSessions(cleanup)
		return nil, err
	}
	if err := a.put(ctx, r); err != nil {
		cleanup := context.WithoutCancel(ctx)
		x.unwind(cleanup)
		x.closeSessions(cleanup)
		return nil, err
	}
	if err := x.kickoff(ctx, g); err != nil {
		a.mu.Lock()
		r.stop(fmt.Sprintf(reasonKickoff, err), a.now())
		a.touch(r.Workspace)
		logError(a.store.Save(ctx, r))
		a.mu.Unlock()
		logError(errors.Join(x.unwind(context.WithoutCancel(ctx))...))
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return publicRun(r), nil
}

func (a *app) show(ctx context.Context, cmd pluginsdk.CommandRun) (shownRun, error) {
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return shownRun{}, err
	}
	r, err := a.lookup(cmd.WorkspaceID, args.ID)
	if err != nil {
		return shownRun{}, err
	}
	sessions := a.sessionMap(ctx, cmd.WorkspaceID)
	spec := r.Spec
	r.Spec = nil
	out := shownRun{Run: r}
	a.mu.Lock()
	pending := maps.Clone(a.pending)
	a.mu.Unlock()
	for _, n := range r.Nodes {
		if n.Step {
			last := r.lastStep(n.ID)
			shown := shownNode{RunNode: n, Status: stepStatus(last), Last: last}
			if spec != nil {
				if node := spec.node(n.ID); node != nil {
					shown.Command = cmpCommand(node)
				}
			}
			if p, ok := pending[r.ID+"/"+n.ID]; ok {
				shown.Pending = pendingOf(p)
			}
			out.Nodes = append(out.Nodes, shown)
			continue
		}
		s := sessions[n.Session]
		shown := shownNode{RunNode: n, Title: s.Title, Status: nodeStatus(s, sessions != nil)}
		outcome, spent, open := r.agentMoves(n.ID)
		shown.DurationMs = spent.Milliseconds()
		switch shown.Status {
		case "running":
			if !open.IsZero() {
				shown.StartedAt = &open
			}
		case "waiting", "error":
		default:
			shown.Outcome = outcome
		}
		out.Nodes = append(out.Nodes, shown)
	}
	return out, nil
}

func (a *app) stopRun(ctx context.Context, cmd pluginsdk.CommandRun, forget bool) (*Run, error) {
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return nil, err
	}
	r, err := a.lookup(cmd.WorkspaceID, args.ID)
	if err != nil {
		return nil, err
	}
	if err := a.authorize(ctx, cmd, r); err != nil {
		return nil, err
	}
	if r.Status == statusRunning {
		x := &runner{call: a.runCommand, run: r}
		if err := errors.Join(x.unwind(ctx)...); err != nil {
			return nil, err
		}
	}
	defer a.cancelSteps(context.WithoutCancel(ctx), r.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	live, ok := a.runs[r.ID]
	if !ok {
		return nil, unknownRun(r.ID)
	}
	if forget {
		delete(a.runs, r.ID)
		delete(a.stepSel, r.ID)
		a.touch(r.Workspace)
		removeOutput(r.ID)
		return nil, a.store.Remove(ctx, live)
	}
	if live.Status == statusRunning {
		live.stop(reasonStopped, a.now())
		a.touch(r.Workspace)
		if err := a.store.Save(ctx, live); err != nil {
			return nil, err
		}
	}
	return publicRun(live), nil
}

func (a *app) put(ctx context.Context, r *Run) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Number == 0 {
		r.Number = a.nextNumber(r)
	}
	if err := a.store.Save(ctx, r); err != nil {
		return err
	}
	a.runs[r.ID] = r
	a.touch(r.Workspace)
	return nil
}

func (a *app) nextNumber(r *Run) int {
	n := 0
	for _, o := range a.runs {
		if o.Workspace == r.Workspace && o.Graph == r.Graph {
			n = max(n, o.Number)
		}
	}
	return n + 1
}

func (a *app) numberRuns(ctx context.Context) {
	runs := slices.Collect(maps.Values(a.runs))
	slices.SortFunc(runs, func(x, y *Run) int { return cmp.Or(x.StartedAt.Compare(y.StartedAt), cmp.Compare(x.ID, y.ID)) })
	for _, r := range runs {
		if r.Number == 0 {
			r.Number = a.nextNumber(r)
			logError(a.store.Save(ctx, r))
		}
	}
}

func (a *app) lookup(workspace, id string) (*Run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.runs[id]
	if !ok || r.Workspace != workspace {
		return nil, unknownRun(id)
	}
	return cloneRun(r), nil
}

func nodeStatus(s api.Session, known bool) string {
	switch {
	case !known:
		return "unknown"
	case s.ID == "":
		return "gone"
	}
	return cmp.Or(s.Status, "idle")
}

func (a *app) reveal(ctx context.Context, cmd pluginsdk.CommandRun) error {
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return err
	}
	if _, err := a.lookup(cmd.WorkspaceID, args.ID); err != nil {
		return err
	}
	if err := a.host.StorageSet(ctx, selectedPrefix+cmd.WorkspaceID, args.ID); err != nil {
		return err
	}
	a.invalidate(cmd.WorkspaceID)
	a.flush(ctx)
	return a.host.Focus(ctx, cmd.WorkspaceID, sectionID, mainSlot)
}

func unknownRun(id string) error {
	return fmt.Errorf("unknown graph run %q", id)
}

func cloneRun(r *Run) *Run {
	c := *r
	c.Nodes = slices.Clone(r.Nodes)
	c.Edges = slices.Clone(r.Edges)
	c.Moves = slices.Clone(r.Moves)
	for i := range c.Moves {
		c.Moves[i].Fired = slices.Clone(c.Moves[i].Fired)
		if st := c.Moves[i].Step; st != nil {
			copied := *st
			c.Moves[i].Step = &copied
		}
	}
	c.Held = slices.Clone(r.Held)
	c.Extra = maps.Clone(r.Extra)
	return &c
}

func publicRun(r *Run) *Run {
	c := cloneRun(r)
	c.Spec = nil
	return c
}

func sortRuns(runs []*Run) {
	slices.SortFunc(runs, func(x, y *Run) int {
		return cmp.Or(y.StartedAt.Compare(x.StartedAt), cmp.Compare(x.ID, y.ID))
	})
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
