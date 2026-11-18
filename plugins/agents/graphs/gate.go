package graphs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	outApproved      = "approved"
	outChanges       = "changes"
	errGateUndecided = "%s can't decide a landing: no node reports approved and changes"
)

var (
	ErrGateGraphChanged = errors.New("the gate's graph changed: waiting for the user to trust it")
	errGateNotRunning   = errors.New("the gate's run is not running")
)

type GateNode struct {
	Title   string
	Run     string
	Model   string
	Effort  string
	Verdict bool
}

type GateRun struct {
	Workspace string
	Graph     string
	Pin       string
	Repo      string
	Input     string
	Other     string
	Dir       string
	NewNode   func(ctx context.Context, n GateNode) (string, error)
}

func (e *Engine) GateGraph(_ context.Context, repo, name string) (string, bool, error) {
	g, _, err := loadGraph(gateDirs(repo), e.a.home, name)
	if err != nil {
		return "", false, err
	}
	return graphHash(g), g.decides(), nil
}

func (e *Engine) GateSteps(_ context.Context, repo, name string) bool {
	g, _, err := loadGraph(gateDirs(repo), e.a.home, name)
	return err == nil && slices.ContainsFunc(g.Nodes, func(n Node) bool { return n.step() })
}

func GateUndecided(name string) error {
	return fmt.Errorf(errGateUndecided, name)
}

func BuiltinHash(name string) string {
	data, err := builtinGraphs.ReadFile(builtinPath(name))
	if err != nil {
		return ""
	}
	g, err := parseGraph(name, data)
	if err != nil {
		return ""
	}
	return graphHash(g)
}

func (e *Engine) StartGate(ctx context.Context, gr GateRun) (string, string, error) {
	a := e.a
	if err := a.waitReady(ctx); err != nil {
		return "", "", err
	}
	defer a.flush(ctx)
	g, from, err := loadGraph(gateDirs(gr.Repo), a.home, gr.Graph)
	if err != nil {
		return "", "", err
	}
	if graphHash(g) != gr.Pin {
		return "", "", ErrGateGraphChanged
	}
	if !g.decides() {
		return "", "", GateUndecided(gr.Graph)
	}
	r, err := newRun(g, gr.Workspace, "", false, "", a.now())
	if err != nil {
		return "", "", err
	}
	r.Input, r.Gate, r.GateDir, r.Builtin, r.User, r.Dir = gr.Input, true, gr.Dir, from == fromBuiltin, from == fromUser, graphDir(gateDirs(gr.Repo), gr.Graph)
	r.GatePin, r.GateOther = gr.Pin, gr.Other
	for i := range r.Edges {
		r.Edges[i].Owned = true
	}
	x := &runner{call: a.runCommand, run: r, started: a.nodeStarted(r.ID), live: a.running(r), stopped: a.stoppedLaunching(r.ID), newNode: gr.NewNode}
	a.launch(r)
	defer a.launched(r.ID)
	if err := x.start(ctx, g); err != nil {
		x.abandon(ctx)
		return "", "", err
	}
	if err := a.put(ctx, r); err != nil {
		x.abandon(ctx)
		return "", "", err
	}
	if err := x.kickoff(ctx, g); err != nil {
		a.mu.Lock()
		if r.Status == statusRunning {
			a.stop(r, fmt.Sprintf(reasonKickoff, err), a.now())
		}
		a.touch(r.Workspace)
		logError(a.store.Save(ctx, r))
		a.mu.Unlock()
		x.abandon(ctx)
		return "", "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return r.ID, r.verdictSession(), nil
}

func (e *Engine) PromptGate(ctx context.Context, run, verdictText, otherText string) error {
	a := e.a
	if err := a.waitReady(ctx); err != nil {
		return err
	}
	defer a.flush(ctx)
	a.mu.Lock()
	r := a.runs[run]
	if r == nil || !r.Gate || r.Status != statusRunning || r.Spec == nil {
		a.mu.Unlock()
		return errGateNotRunning
	}
	r.Input, r.GateOther = verdictText, otherText
	var starts []promptArgs
	for _, n := range r.Nodes {
		if !n.Start || n.Session == "" {
			continue
		}
		r.startMove(n.ID, a.now())
		text := otherText
		if spec := r.Spec.node(n.ID); spec != nil && spec.decides() {
			text = verdictText
		}
		starts = append(starts, promptArgs{Session: n.Session, Text: text})
	}
	a.touch(r.Workspace)
	logError(a.store.Save(ctx, r))
	x := &runner{call: a.runCommand, run: cloneRun(r)}
	a.mu.Unlock()
	for _, p := range starts {
		if err := x.do(ctx, "agents.prompt", p, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) GateRunOf(ctx context.Context, run, graph, pin string) bool {
	a := e.a
	if a.waitReady(ctx) != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[run]
	return r != nil && r.Gate && r.Graph == graph && r.GatePin == pin
}

func (e *Engine) GateSessions(ctx context.Context, run string) []string {
	a := e.a
	if a.waitReady(ctx) != nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[run]
	if r == nil || !r.Gate {
		return nil
	}
	var out []string
	for _, n := range r.Nodes {
		if !n.Step {
			out = append(out, n.Session)
		}
	}
	return out
}

func (e *Engine) StopGate(ctx context.Context, run string) error {
	a := e.a
	if err := a.waitReady(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	r := a.runs[run]
	if r == nil || !r.Gate || r.Status != statusRunning {
		a.mu.Unlock()
		return nil
	}
	a.stop(r, reasonStopped, a.now())
	a.touch(r.Workspace)
	err := a.store.Save(ctx, r)
	stopped := cloneRun(r)
	a.mu.Unlock()
	a.finish(context.WithoutCancel(ctx), stopped)
	a.flush(ctx)
	return err
}

func (e *Engine) GateRunning(ctx context.Context, run string) bool {
	a := e.a
	if a.waitReady(ctx) != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[run]
	return r != nil && r.Gate && r.Status == statusRunning
}

func (e *Engine) GateStepsIdle(ctx context.Context, run string) bool {
	a := e.a
	if a.waitReady(ctx) != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[run]
	return r != nil && !slices.ContainsFunc(r.Moves, func(m Move) bool {
		n := r.node(m.Node)
		return m.Open && n != nil && n.Step
	})
}

func (e *Engine) GateOutcome(ctx context.Context, run, session, outcome string) {
	a := e.a
	a.mu.Lock()
	r := a.runs[run]
	if r == nil || !r.Gate || r.Status != statusRunning || r.Spec == nil {
		a.mu.Unlock()
		return
	}
	n := r.nodeBySession(session)
	if n == nil {
		a.mu.Unlock()
		return
	}
	before := len(r.Moves)
	r.outcome(n.ID, outcome, a.now())
	owed := r.owedSince(before, ownedCause{Outcome: outcome})
	a.touch(r.Workspace)
	logError(a.store.Save(ctx, r))
	a.mu.Unlock()
	for _, d := range owed {
		a.deliver(ctx, d)
	}
	a.flush(ctx)
}

func (a *app) stop(r *Run, reason string, now time.Time) {
	r.stop(reason, now)
	if r.Gate {
		workspace, id := r.Workspace, r.ID
		a.spawn(func() { a.gateEnded(workspace, id) })
	}
}

func (r *Run) verdictSession() string {
	if r.Spec == nil {
		return ""
	}
	for i, n := range r.Spec.Nodes {
		if n.decides() && i < len(r.Nodes) {
			return r.Nodes[i].Session
		}
	}
	return ""
}

func (g *Graph) deciders() []string {
	var out []string
	for _, n := range g.Nodes {
		if n.decides() {
			out = append(out, n.ID)
		}
	}
	return out
}

func (g *Graph) decides() bool {
	return slices.ContainsFunc(g.Nodes, func(n Node) bool { return n.decides() })
}

func (n *Node) decides() bool {
	return !n.step() && slices.Contains(n.Outcomes, outApproved) && slices.Contains(n.Outcomes, outChanges)
}

func gateDirs(repo string) []string {
	if repo == "" {
		return nil
	}
	return []string{repo}
}

func graphHash(g *Graph) string {
	data, _ := json.Marshal(g)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func NeedAction(action string) bool {
	return action == actApprove || action == actContinue
}

func (e *Engine) NeedCard(run string) (sdui.Node, bool) {
	a := e.a
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[run]
	if r == nil || !r.Gate {
		return sdui.Node{}, false
	}
	need := r.need()
	switch {
	case need == nil:
		return sdui.Node{}, false
	case need.Kind == needCap:
		cont := automation.ActionNode(r.ID, automation.DetailAction{Action: actContinue, Label: "Continue", Icon: "play.fill", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"})
		return sdui.VStack(capCard(r, need), sdui.HStack(sdui.Spacer(), cont)), true
	}
	var p *pendingInfo
	if ps, ok := a.pending[r.ID+"/"+need.Node]; ok {
		p = pendingOf(ps)
	}
	return approvalCard(r, need, p, a.home), true
}
