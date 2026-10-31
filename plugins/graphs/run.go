package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	runPrefix     = "run/"
	runIDPrefix   = "gr_"
	statusRunning = "running"
	statusStopped = "stopped"
	turnTopic     = "agents.turn"
	eventPrefix   = "event."
	maxInput      = 16 << 10
)

var errInputPlaceholder = errors.New("input: must not contain {{")

type Run struct {
	ID        string     `json:"id"`
	Graph     string     `json:"graph"`
	Title     string     `json:"title"`
	Workspace string     `json:"workspace"`
	Input     string     `json:"input,omitempty"`
	Agent     bool       `json:"agent,omitempty"`
	Caller    string     `json:"caller,omitempty"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	MaxTurns  int        `json:"maxTurns"`
	Turns     int        `json:"turns"`
	StartedAt time.Time  `json:"startedAt"`
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`
	Nodes     []RunNode  `json:"nodes"`
	Edges     []RunEdge  `json:"edges"`
	Spec      *Graph     `json:"spec,omitempty"`
}

type RunNode struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Start     bool   `json:"start,omitempty"`
	Session   string `json:"session"`
	HandedOff bool   `json:"handedOff,omitempty"`
}

type RunEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	On      string `json:"on"`
	Outcome string `json:"outcome,omitempty"`
	Trigger string `json:"trigger"`
}

type commandRequest struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args,omitempty"`
	Agent     bool            `json:"agent"`
	Caller    string          `json:"caller,omitempty"`
}

type newArgs struct {
	Title  string `json:"title"`
	Branch string `json:"branch,omitempty"`
}

type newResult struct {
	Session string `json:"session"`
}

type promptArgs struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

type triggerArgs struct {
	Name    string `json:"name"`
	On      string `json:"on"`
	Match   string `json:"match"`
	Command string `json:"command"`
	Args    string `json:"args"`
	Owner   string `json:"owner"`
}

type updateArgs struct {
	ID    string `json:"id"`
	Match string `json:"match"`
	Args  string `json:"args"`
}

type triggerResult struct {
	ID string `json:"id"`
}

type idArgs struct {
	ID string `json:"id"`
}

type sessionArgs struct {
	Session string `json:"session"`
}

type runner struct {
	call func(ctx context.Context, req commandRequest) (json.RawMessage, error)
	run  *Run
}

func newRun(g *Graph, workspace, input string, agent bool, caller string, now time.Time) (*Run, error) {
	if strings.Contains(input, "{{") {
		return nil, errInputPlaceholder
	}
	if len(input) > maxInput {
		return nil, fmt.Errorf("input: at most %d KiB", maxInput>>10)
	}
	id, err := newRunID()
	if err != nil {
		return nil, err
	}
	r := &Run{
		ID:        id,
		Graph:     g.Name,
		Title:     graphTitle(g),
		Workspace: workspace,
		Input:     input,
		Agent:     agent,
		Caller:    caller,
		Status:    statusRunning,
		MaxTurns:  g.maxTurns(),
		StartedAt: now,
		Spec:      g,
	}
	for _, n := range g.Nodes {
		r.Nodes = append(r.Nodes, RunNode{ID: n.ID, Role: n.label(), Start: n.Start})
	}
	for _, e := range g.Edges {
		r.Edges = append(r.Edges, RunEdge{From: e.From, To: e.To, On: e.On, Outcome: e.Outcome})
	}
	return r, nil
}

func (x *runner) start(ctx context.Context, g *Graph) error {
	for i, n := range g.Nodes {
		args := newArgs{Title: x.run.Title + " · " + n.label(), Branch: x.render(n.Branch)}
		var out newResult
		if err := x.do(ctx, "agents.new", args, &out); err != nil {
			return fmt.Errorf("start node %s: %w", n.ID, err)
		}
		x.run.Nodes[i].Session = out.Session
	}
	for i, e := range g.Edges {
		args, err := x.edgeTrigger(g, e)
		if err != nil {
			return err
		}
		var out triggerResult
		if err := x.do(ctx, "triggers.add", args, &out); err != nil {
			return fmt.Errorf("connect %s → %s: %w", e.From, e.To, err)
		}
		x.run.Edges[i].Trigger = out.ID
	}
	return nil
}

func (x *runner) kickoff(ctx context.Context, g *Graph) error {
	for i, n := range g.Nodes {
		if !n.Start {
			continue
		}
		text := x.brief(&n)
		if x.run.Input != "" && !strings.Contains(n.Prompt, "{{input}}") {
			text += "\n\nTask:\n" + x.run.Input
		}
		if err := x.do(ctx, "agents.prompt", promptArgs{Session: x.run.Nodes[i].Session, Text: text}, nil); err != nil {
			return fmt.Errorf("prompt node %s: %w", n.ID, err)
		}
	}
	return nil
}

func (x *runner) repoint(ctx context.Context) []error {
	var errs []error
	for i, e := range x.run.Spec.Edges {
		args, err := x.edgeTrigger(x.run.Spec, e)
		if err == nil {
			err = x.do(ctx, "triggers.update", updateArgs{ID: x.run.Edges[i].Trigger, Match: args.Match, Args: args.Args}, nil)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("re-point %s → %s: %w", e.From, e.To, err))
		}
	}
	return errs
}

func (x *runner) unwind(ctx context.Context) []error {
	var errs []error
	for _, e := range x.run.Edges {
		if e.Trigger == "" {
			continue
		}
		if err := x.do(ctx, "triggers.delete", idArgs{ID: e.Trigger}, nil); err != nil && !isUnknownTrigger(err) {
			errs = append(errs, fmt.Errorf("delete trigger %s: %w", e.Trigger, err))
		}
	}
	return errs
}

func (x *runner) closeSessions(ctx context.Context) {
	for _, n := range x.run.Nodes {
		if n.Session == "" {
			continue
		}
		req := commandRequest{ID: "agents.close", ProjectID: x.run.Workspace}
		req.Args, _ = json.Marshal(sessionArgs{Session: n.Session})
		_, _ = x.call(ctx, req)
	}
}

func (x *runner) edgeTrigger(g *Graph, e Edge) (triggerArgs, error) {
	from, to := x.run.node(e.From), x.run.node(e.To)
	on, match := turnTopic, map[string]any{"session": from.Session}
	switch e.On {
	case onDone:
		match["status"] = "done"
		if g.hasEdge(e.From, onAsks) {
			match["asks"] = false
		}
	case onAsks:
		match["status"], match["asks"] = "done", true
	case onError:
		match["status"] = "error"
	case onOutcome:
		on = eventPrefix + x.run.outcomeEvent(e.From)
		match = map[string]any{"by": from.Session, "outcome": e.Outcome}
	}
	text := x.render(e.Text)
	if text == "" {
		text = x.defaultEdgeText(from, e)
	}
	if target := g.node(e.To); !target.Start || to.HandedOff {
		text = x.brief(target) + "\n\n" + text
	} else {
		text = x.header(target) + "\n\n" + text
	}
	matchText, err := json.Marshal(match)
	if err != nil {
		return triggerArgs{}, err
	}
	argsText, err := json.Marshal(promptArgs{Session: to.Session, Text: text})
	if err != nil {
		return triggerArgs{}, err
	}
	return triggerArgs{
		Name:    fmt.Sprintf("%s %s: %s → %s", x.run.Graph, x.run.ID, e.From, e.To),
		On:      on,
		Match:   string(matchText),
		Command: "agents.prompt",
		Args:    string(argsText),
		Owner:   "self",
	}, nil
}

func (x *runner) brief(n *Node) string {
	var b strings.Builder
	b.WriteString(x.header(n))
	b.WriteString("\n\n")
	b.WriteString(x.render(n.Prompt))
	var peers []string
	for _, rn := range x.run.Nodes {
		if rn.ID != n.ID {
			peers = append(peers, fmt.Sprintf("%s (%s) is session %s", rn.ID, rn.Role, rn.Session))
		}
	}
	if len(peers) > 0 {
		b.WriteString("\n\nOther nodes of this run: " + strings.Join(peers, "; ") + ". Clubmate passes your results on along the graph's edges; read another node's work with the clubmate tool agents_transcript.")
	}
	b.WriteString("\n\nYour reply goes to the next node of the graph, not to a person: do not end it with a question unless you need the answer to go on.")
	if len(n.Outcomes) > 0 {
		payloads := make([]string, 0, len(n.Outcomes))
		for _, o := range n.Outcomes {
			payloads = append(payloads, strconv.Quote(`{"outcome":`+strconv.Quote(o)+`}`))
		}
		fmt.Fprintf(&b, "\n\nWhen you finish, report the outcome before ending your turn: call the clubmate tool triggers_emit with name %q and payload one of %s.",
			x.run.outcomeEvent(n.ID), strings.Join(payloads, ", "))
	}
	return b.String()
}

func (x *runner) header(n *Node) string {
	return fmt.Sprintf("[Agent graph %s, run %s: you are node %s (%s)]", x.run.Graph, x.run.ID, n.ID, n.label())
}

func (x *runner) defaultEdgeText(from *RunNode, e Edge) string {
	who := fmt.Sprintf("%s (%s, session %s)", from.ID, from.Role, from.Session)
	transcript := fmt.Sprintf("the clubmate tool agents_transcript {\"session\":%q}", from.Session)
	if e.On == onOutcome {
		return who + " reported outcome " + e.Outcome + ". Read its last reply with " + transcript + " and act on it."
	}
	reply := "Its last reply (possibly cut; the whole transcript is in " + transcript + "):\n\n{{payload.reply}}"
	switch e.On {
	case onError:
		return who + " stopped with an error. " + reply
	case onAsks:
		return who + " asks a question. Answer it with agents_prompt to that session if you can. " + reply
	}
	return who + " finished its turn. Act on it. " + reply
}

func (x *runner) render(s string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[1]
		switch name {
		case "input":
			return x.run.Input
		case "run":
			return x.run.ID
		case "graph":
			return x.run.Graph
		}
		if id, ok := strings.CutPrefix(name, "node."); ok {
			if n := x.run.node(id); n != nil {
				return n.Session
			}
		}
		return m
	})
}

func (x *runner) do(ctx context.Context, command string, args, out any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	req := commandRequest{ID: command, ProjectID: x.run.Workspace, Args: raw, Agent: x.run.Agent, Caller: x.run.Caller}
	res, err := x.call(ctx, req)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(res, out)
}

func (r *Run) node(id string) *RunNode {
	for i := range r.Nodes {
		if r.Nodes[i].ID == id {
			return &r.Nodes[i]
		}
	}
	return nil
}

func (r *Run) outcomeEvent(node string) string {
	return "graph." + r.ID + "." + node
}

func (r *Run) nodeBySession(session string) *RunNode {
	for i := range r.Nodes {
		if r.Nodes[i].Session == session {
			return &r.Nodes[i]
		}
	}
	return nil
}

func (r *Run) stop(reason string, now time.Time) {
	r.Status, r.Reason, r.StoppedAt = statusStopped, reason, &now
}

func graphTitle(g *Graph) string {
	if g.Title != "" {
		return g.Title
	}
	return g.Name
}

func isUnknownTrigger(err error) bool {
	return strings.Contains(err.Error(), "unknown trigger")
}

func newRunID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return runIDPrefix + hex.EncodeToString(b), nil
}

func runKey(r *Run) string {
	return runPrefix + r.Workspace + "/" + r.ID
}
