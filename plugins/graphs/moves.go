package main

import (
	"slices"
	"time"

	"clubmate/plugins/agents/api"
)

type Move struct {
	Node  string    `json:"node"`
	Cause int       `json:"cause"`
	Edge  int       `json:"edge"`
	User  bool      `json:"user,omitempty"`
	Fired []int     `json:"fired,omitempty"`
	End   string    `json:"end,omitempty"`
	At    time.Time `json:"at"`
	Turn  int       `json:"turn,omitempty"`
	Open  bool      `json:"open,omitempty"`
	Step  *StepMove `json:"step,omitempty"`
	Held  bool      `json:"held,omitempty"`
}

type StepMove struct {
	Status     string    `json:"status"`
	Outcome    string    `json:"outcome,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	ExitCode   *int      `json:"exitCode,omitempty"`
	AllowedBy  string    `json:"allowedBy,omitempty"`
	Dir        string    `json:"dir,omitempty"`
	Command    string    `json:"command,omitempty"`
	Tail       string    `json:"tail,omitempty"`
	Output     string    `json:"output,omitempty"`
	WaitingAt  time.Time `json:"waitingAt,omitzero"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	EndedAt    time.Time `json:"endedAt,omitzero"`
	Duration   int64     `json:"durationMs,omitempty"`
	CheckFiles []string  `json:"checkFilesChanged,omitempty"`
}

func (r *Run) startMove(node string, now time.Time) {
	r.Moves = append(r.Moves, Move{Node: node, Cause: -1, Edge: -1, At: now, Open: true})
}

func (r *Run) endTurn(p api.Turn, now time.Time) {
	n := r.nodeBySession(p.Session)
	if n == nil || !turnOver(p) {
		return
	}
	i := r.closeMove(n.ID, p.Turn)
	if i < 0 {
		r.Moves = append(r.Moves, Move{Node: n.ID, Cause: -1, Edge: -1, User: true, At: now, Turn: p.Turn})
		i = len(r.Moves) - 1
	}
	if r.Spec == nil {
		return
	}
	for e, edge := range r.Spec.Edges {
		if edge.From == n.ID && edgeMatchesTurn(r.Spec, edge, p) {
			r.fire(i, e, now)
		}
	}
}

func (r *Run) outcome(node, outcome string, now time.Time) {
	if r.Spec == nil {
		return
	}
	i := r.oldestOpen(node)
	if i < 0 {
		r.startMove(node, now)
		i = len(r.Moves) - 1
		r.Moves[i].User = true
	}
	for e, edge := range r.Spec.Edges {
		if edge.From == node && edge.On == onOutcome && edge.Outcome == outcome {
			r.fire(i, e, now)
		}
	}
	if spec := r.Spec.node(node); spec != nil && slices.Contains(spec.End, outcome) {
		r.Moves[i].End = outcome
	}
}

func (r *Run) fire(cause, edge int, now time.Time) {
	r.Moves[cause].Fired = append(r.Moves[cause].Fired, edge)
	r.Moves = append(r.Moves, Move{Node: r.Edges[edge].To, Cause: cause, Edge: edge, At: now, Open: true})
}

func (r *Run) lastEdges() []int {
	var open []int
	last := -1
	for _, m := range r.Moves {
		if m.Edge < 0 {
			continue
		}
		last = m.Edge
		if m.Open && r.Status == statusRunning && !slices.Contains(open, m.Edge) {
			open = append(open, m.Edge)
		}
	}
	if len(open) == 0 && last >= 0 {
		return []int{last}
	}
	return open
}

func (r *Run) edgeCounts() []int {
	counts := make([]int, len(r.Edges))
	for _, m := range r.Moves {
		if m.Edge >= 0 && m.Edge < len(counts) {
			counts[m.Edge]++
		}
	}
	return counts
}

func (r *Run) oldestOpen(node string) int {
	return slices.IndexFunc(r.Moves, func(m Move) bool { return m.Node == node && m.Open })
}

func (r *Run) closeMove(node string, turn int) int {
	i := r.oldestOpen(node)
	if i >= 0 {
		r.Moves[i].Open, r.Moves[i].Turn = false, turn
	}
	return i
}

func turnOver(p api.Turn) bool {
	return p.Status == "done" || p.Status == "error" || p.Status == api.StatusClosed
}

func edgeMatchesTurn(g *Graph, e Edge, p api.Turn) bool {
	switch e.On {
	case onDone:
		return p.Status == "done" && (!p.Asks || !g.hasEdge(e.From, onAsks))
	case onAsks:
		return p.Status == "done" && p.Asks
	case onError:
		return p.Status == "error"
	}
	return false
}
