package graphs

import (
	"context"

	"clubmate/plugins/agents/api"
)

type RunSnapshot struct {
	ID       string
	Graph    string
	Title    string
	Number   int
	Dir      string
	Status   string
	Reason   string
	Turns    int
	MaxTurns int
	Nodes    []SnapshotNode
	Need     *api.RunNeed
}

type SnapshotNode struct {
	ID      string
	Role    string
	Session string
	Step    bool
}

type Definition struct {
	Name       string
	Title      string
	Nodes      int
	Builtin    bool
	Untrusted  bool
	WaitsTrust bool
}

func (e *Engine) Runs(workspace string) []RunSnapshot {
	runs := e.a.workspaceRuns(workspace, false)
	out := make([]RunSnapshot, 0, len(runs))
	for _, r := range runs {
		s := RunSnapshot{ID: r.ID, Graph: r.Graph, Title: r.Title, Number: r.Number, Dir: r.Dir, Status: r.Status, Reason: r.Reason, Turns: r.Turns, MaxTurns: r.MaxTurns, Need: r.need()}
		for _, n := range r.Nodes {
			s.Nodes = append(s.Nodes, SnapshotNode{ID: n.ID, Role: n.Role, Session: n.Session, Step: n.Step})
		}
		out = append(out, s)
	}
	return out
}

func (e *Engine) Definitions(ctx context.Context, workspace string) []Definition {
	a := e.a
	dirs, err := a.graphDirs(ctx, workspace, false, "")
	logError(err)
	files := listGraphs(dirs, a.home)
	untrusted := a.untrustedGraphs(ctx, workspace, dirs, files)
	out := make([]Definition, 0, len(files))
	for _, f := range files {
		if f.Error != "" {
			continue
		}
		d := Definition{Name: f.Name, Title: f.Title, Nodes: f.Nodes, Builtin: f.Builtin, Untrusted: untrusted[f.Name]}
		if d.Untrusted {
			if spec, _, err := loadGraph(dirs, a.home, f.Name); err == nil {
				d.WaitsTrust = a.waitsTrust(workspace, f.Name, stepsHash(spec))
			}
		}
		out = append(out, d)
	}
	return out
}

func trustWaitKey(workspace, graph, hash string) string {
	return workspace + "\n" + graph + "\n" + hash
}

func (a *app) waitsTrust(workspace, graph, hash string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refused[trustWaitKey(workspace, graph, hash)]
}
