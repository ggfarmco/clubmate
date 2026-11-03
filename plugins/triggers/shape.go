package main

import (
	"context"
	"time"

	"clubmate/plugins/internal/automation"
)

const (
	graphsShow   = "graphs.show"
	shapeTimeout = 10 * time.Second
	onOutcome    = "outcome"
)

type graphShape struct {
	Nodes []shapeNode `json:"nodes"`
	Edges []shapeEdge `json:"edges"`
}

type shapeNode struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Start   bool   `json:"start,omitempty"`
	Step    bool   `json:"step,omitempty"`
	Session string `json:"session"`
}

type shapeEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	On      string `json:"on"`
	Outcome string `json:"outcome,omitempty"`
	Trigger string `json:"trigger,omitempty"`
}

func (e shapeEdge) out() string {
	if e.On == onOutcome {
		return e.Outcome
	}
	return e.On
}

func (a *app) shape(workspace string, t *Trigger) *graphShape {
	if t == nil || t.Run == "" || a.host == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.shapes[t.Run]; ok {
		return s
	}
	a.shapes[t.Run] = nil
	run := t.Run
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), shapeTimeout)
		defer cancel()
		var s graphShape
		err := a.host.RunCommand(ctx, graphsShow, workspace, automation.IDArgs{ID: run}, &s)
		a.mu.Lock()
		if err != nil {
			delete(a.shapes, run)
		} else {
			a.shapes[run] = &s
		}
		a.mu.Unlock()
		if err != nil {
			return
		}
		a.registry.Invalidate(workspace)
		a.registry.Flush(ctx)
	}()
	return nil
}
