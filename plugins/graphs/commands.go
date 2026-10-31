package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
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
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
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
	}
	return nil, fmt.Errorf("%w: %q", agentcall.ErrUnknownCommand, cmd.ID)
}

func (a *app) list(ctx context.Context, cmd pluginsdk.CommandRun) (listResult, error) {
	dirs, err := a.graphDirs(ctx, cmd.ProjectID, cmd.Agent, cmd.Caller)
	if err != nil {
		return listResult{}, err
	}
	res := listResult{Graphs: listGraphs(dirs)}
	for _, r := range a.workspaceRuns(cmd.ProjectID) {
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
	if err := decodeArgs(cmd, &args); err != nil {
		return nil, err
	}
	caller := ""
	if cmd.Agent {
		caller = cmd.Caller
	}
	dirs, err := a.graphDirs(ctx, cmd.ProjectID, cmd.Agent, caller)
	if err != nil {
		return nil, err
	}
	g, _, err := loadGraph(dirs, args.Graph)
	if err != nil {
		return nil, err
	}
	r, err := newRun(g, cmd.ProjectID, args.Input, cmd.Agent, caller, a.now())
	if err != nil {
		return nil, err
	}
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
		logError(a.store.save(ctx, r))
		a.mu.Unlock()
		logError(errors.Join(x.unwind(context.WithoutCancel(ctx))...))
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneRun(r), nil
}

func (a *app) show(ctx context.Context, cmd pluginsdk.CommandRun) (shownRun, error) {
	var args idArgs
	if err := decodeArgs(cmd, &args); err != nil {
		return shownRun{}, err
	}
	r, err := a.lookup(cmd.ProjectID, args.ID)
	if err != nil {
		return shownRun{}, err
	}
	sessions := a.sessionMap(ctx, cmd.ProjectID)
	out := shownRun{Run: r}
	for _, n := range r.Nodes {
		s := sessions[n.Session]
		out.Nodes = append(out.Nodes, shownNode{RunNode: n, Title: s.Title, Status: nodeStatus(s, sessions != nil)})
	}
	return out, nil
}

func (a *app) stopRun(ctx context.Context, cmd pluginsdk.CommandRun, forget bool) (*Run, error) {
	var args idArgs
	if err := decodeArgs(cmd, &args); err != nil {
		return nil, err
	}
	r, err := a.lookup(cmd.ProjectID, args.ID)
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
	a.mu.Lock()
	defer a.mu.Unlock()
	live, ok := a.runs[r.ID]
	if !ok {
		return nil, unknownRun(r.ID)
	}
	if forget {
		delete(a.runs, r.ID)
		a.touch(r.Workspace)
		return nil, a.store.remove(ctx, live)
	}
	if live.Status == statusRunning {
		live.stop(reasonStopped, a.now())
		a.touch(r.Workspace)
		if err := a.store.save(ctx, live); err != nil {
			return nil, err
		}
	}
	return cloneRun(live), nil
}

func (a *app) put(ctx context.Context, r *Run) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.save(ctx, r); err != nil {
		return err
	}
	a.runs[r.ID] = r
	a.touch(r.Workspace)
	return nil
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

func nodeStatus(s agentcall.Session, known bool) string {
	switch {
	case !known:
		return "unknown"
	case s.ID == "":
		return "gone"
	case s.Closed:
		return "closed"
	}
	return cmp.Or(s.Status, "idle")
}

func unknownRun(id string) error {
	return fmt.Errorf("unknown graph run %q", id)
}

func decodeArgs(cmd pluginsdk.CommandRun, v any) error {
	if len(cmd.Args) == 0 {
		return nil
	}
	if err := json.Unmarshal(cmd.Args, v); err != nil {
		return fmt.Errorf("args: %w", err)
	}
	return nil
}

func cloneRun(r *Run) *Run {
	c := *r
	c.Nodes = slices.Clone(r.Nodes)
	c.Edges = slices.Clone(r.Edges)
	return &c
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
