package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/gitrepo"
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

func repoKey(prefix, root, id string) string {
	sum := sha256.Sum256([]byte(root + "\n" + id))
	return prefix + hex.EncodeToString(sum[:])
}

func repoRoot(ctx context.Context, dir string) string {
	top, ok := gitrepo.RepoRoot(dir)
	if !ok || !filepath.IsAbs(dir) {
		return dir
	}
	trees, err := gitrepo.Worktrees(ctx, top)
	if err != nil {
		logError(fmt.Errorf("trust: worktrees of %s: %w", dir, err))
		return dir
	}
	if len(trees) == 0 || trees[0].Bare || !slices.ContainsFunc(trees, func(t gitrepo.Worktree) bool { return inside(dir, t.Path) }) {
		return dir
	}
	return trees[0].Path
}

func rootOf(ctx context.Context, dir string) func() string {
	return sync.OnceValue(func() string { return repoRoot(ctx, dir) })
}

func trustDir(dirs []string, name string) string {
	if dir := graphDir(dirs, name); dir != "" || len(dirs) == 0 {
		return dir
	}
	return dirs[0]
}

func (a *app) trustState(ctx context.Context, root func() string, g *Graph) (bool, []string, error) {
	hash := stepsHash(g)
	if hash == "" {
		return false, nil, nil
	}
	var trusted bool
	found, err := a.kvGet(ctx, repoKey(trustPrefix, root(), hash), &trusted)
	if err != nil {
		return true, nil, err
	}
	if found && trusted {
		return false, nil, nil
	}
	last, _, err := a.lastTrusted(ctx, root(), g.Name)
	if err != nil {
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
	g, _, err := loadGraph(dirs, a.home, args.Graph)
	if err != nil {
		return err
	}
	hash := stepsHash(g)
	if hash == "" {
		return nil
	}
	if args.Hash != "" && args.Hash != hash {
		return fmt.Errorf("graph %s changed since it was shown: look at its steps again", args.Graph)
	}
	root := repoRoot(ctx, trustDir(dirs, args.Graph))
	if err := a.kvSet(ctx, repoKey(trustPrefix, root, hash), true); err != nil {
		return err
	}
	a.mu.Lock()
	clear(a.untrusted)
	a.mu.Unlock()
	return a.kvSet(ctx, repoKey(trustedPrefix, root, g.Name), trustedSteps{Hash: hash, Steps: stepsOf(g), Routing: routingOf(g), At: a.now()})
}

func (a *app) untrustedGraphs(ctx context.Context, workspace string, dirs []string, graphs []graphFile) map[string]bool {
	out := map[string]bool{}
	for _, g := range graphs {
		if g.Error != "" {
			continue
		}
		spec, _, err := loadGraph(dirs, a.home, g.Name)
		if err != nil {
			continue
		}
		key := workspace + "\n" + g.Name + "\n" + stepsHash(spec)
		a.mu.Lock()
		untrusted, ok := a.untrusted[key]
		a.mu.Unlock()
		if !ok {
			if untrusted, _, err = a.trustState(ctx, rootOf(ctx, trustDir(dirs, g.Name)), spec); err != nil {
				logError(err)
				continue
			}
			a.mu.Lock()
			a.untrusted[key] = untrusted
			a.mu.Unlock()
		}
		if untrusted {
			out[g.Name] = true
		}
	}
	return out
}

func (a *app) storageGet(ctx context.Context, key string, out any) (bool, error) {
	return a.host.StorageGet(ctx, key, out)
}

func (a *app) storageSet(ctx context.Context, key string, v any) error {
	return a.host.StorageSet(ctx, key, v)
}

func (a *app) lastTrusted(ctx context.Context, root, name string) (trustedSteps, bool, error) {
	var last trustedSteps
	found, err := a.kvGet(ctx, repoKey(trustedPrefix, root, name), &last)
	return last, found && last.Hash != "", err
}
