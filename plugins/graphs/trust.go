package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	trustPrefix   = "trust/"
	trustedPrefix = "trusted/"
	errUntrusted  = "untrusted: %s has shell steps you haven't trusted (graphs.trust)"
)

type trustedSteps struct {
	Hash    string    `json:"hash"`
	Steps   []Node    `json:"steps"`
	Routing string    `json:"routing"`
	At      time.Time `json:"at"`
}

type nodeShape struct {
	ID    string   `json:"id"`
	Step  bool     `json:"step,omitempty"`
	Start bool     `json:"start,omitempty"`
	End   []string `json:"end,omitempty"`
}

type trustArgs struct {
	Graph string `json:"graph"`
	Hash  string `json:"hash,omitempty"`
}

func stepsOf(g *Graph) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if n.step() {
			out = append(out, Node{ID: n.ID, Run: n.Run, Command: n.Command, Args: n.Args, In: n.In, Timeout: n.Timeout, Exits: n.Exits})
		}
	}
	return out
}

func routingOf(g *Graph) string {
	shape := struct {
		Nodes []nodeShape `json:"nodes"`
		Edges []Edge      `json:"edges"`
	}{}
	for _, n := range g.Nodes {
		shape.Nodes = append(shape.Nodes, nodeShape{ID: n.ID, Step: n.step(), Start: n.Start, End: n.End})
	}
	for _, e := range g.Edges {
		e.Text = ""
		shape.Edges = append(shape.Edges, e)
	}
	data, _ := json.Marshal(shape)
	return string(data)
}

func stepsHash(g *Graph) string {
	steps := stepsOf(g)
	if len(steps) == 0 {
		return ""
	}
	data, _ := json.Marshal(steps)
	sum := sha256.Sum256(append(data, routingOf(g)...))
	return hex.EncodeToString(sum[:])
}

func (a *app) trustKey(ctx context.Context, workspace, dir, hash string) string {
	sum := sha256.Sum256([]byte(a.repoRoot(ctx, workspace, dir) + "\n" + hash))
	return trustPrefix + hex.EncodeToString(sum[:])
}

func (a *app) repoRoot(ctx context.Context, workspace, dir string) string {
	raw, err := a.runCommand(ctx, automation.CommandRequest{ID: "git.worktrees", WorkspaceID: workspace, Args: json.RawMessage(`{}`)})
	var trees []worktreeInfo
	if err == nil {
		err = json.Unmarshal(raw, &trees)
	}
	if err != nil {
		logError(fmt.Errorf("trust: git.worktrees: %w", err))
		return dir
	}
	return cmp.Or(mainCheckout(trees, dir), dir)
}

func (a *app) trustState(ctx context.Context, workspace, dir string, g *Graph, builtin bool) (bool, []string, error) {
	hash := stepsHash(g)
	if builtin || hash == "" {
		return false, nil, nil
	}
	var trusted bool
	found, err := a.kvGet(ctx, a.trustKey(ctx, workspace, dir, hash), &trusted)
	if err != nil {
		return true, nil, err
	}
	if found && trusted {
		return false, nil, nil
	}
	var last trustedSteps
	if _, err := a.kvGet(ctx, trustedPrefix+g.Name, &last); err != nil {
		return true, nil, err
	}
	var changed []string
	for _, n := range stepsOf(g) {
		i := slices.IndexFunc(last.Steps, func(o Node) bool { return o.ID == n.ID })
		if i < 0 || !sameStep(last.Steps[i], n) {
			changed = append(changed, n.ID)
		}
	}
	if last.Routing != "" && last.Routing != routingOf(g) {
		changed = append(changed, "edges")
	}
	return true, changed, nil
}

func sameStep(x, y Node) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}

func (a *app) trustGraph(ctx context.Context, cmd pluginsdk.CommandRun) error {
	if cmd.Agent {
		return fmt.Errorf("%w: %q", agentcall.ErrUnknownCommand, cmd.ID)
	}
	var args trustArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return err
	}
	dirs, err := a.graphDirs(ctx, cmd.WorkspaceID, false, "")
	if err != nil {
		return err
	}
	g, builtin, err := loadGraph(dirs, args.Graph)
	if err != nil {
		return err
	}
	hash := stepsHash(g)
	if builtin || hash == "" {
		return nil
	}
	if args.Hash != "" && args.Hash != hash {
		return fmt.Errorf("graph %s changed since it was shown: look at its steps again", args.Graph)
	}
	if err := a.kvSet(ctx, a.trustKey(ctx, cmd.WorkspaceID, graphDir(dirs, args.Graph), hash), true); err != nil {
		return err
	}
	return a.kvSet(ctx, trustedPrefix+g.Name, trustedSteps{Hash: hash, Steps: stepsOf(g), Routing: routingOf(g), At: a.now()})
}

func (a *app) storageGet(ctx context.Context, key string, out any) (bool, error) {
	return a.host.StorageGet(ctx, key, out)
}

func (a *app) storageSet(ctx context.Context, key string, v any) error {
	return a.host.StorageSet(ctx, key, v)
}
