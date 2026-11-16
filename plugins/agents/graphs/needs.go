package graphs

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"clubmate/plugins/agents/api"
)

const (
	needStep = "step"
	needCap  = "cap"
)

type runNeed = api.RunNeed

type pendingInfo struct {
	Command string `json:"command"`
	Dir     string `json:"dir"`
	Main    string `json:"main,omitempty"`
	Rule    string `json:"rule,omitempty"`
}

func (r *Run) need() *runNeed {
	if r.Status != statusRunning {
		return nil
	}
	var best *runNeed
	pick := func(n runNeed) {
		if best == nil || n.Since.Before(best.Since) {
			best = &n
		}
	}
	title := cmp.Or(r.Title, r.Graph)
	for _, m := range r.Moves {
		if m.Step == nil || m.Step.Status != stepWaiting {
			continue
		}
		role := roleOf(r, m.Node)
		pick(runNeed{Run: r.ID, Title: title, Number: r.Number, Kind: needStep, Request: "step-" + m.Node, Node: m.Node, Role: role, Text: role + " wants to run " + m.Step.Command, Command: m.Step.Command, Since: m.Step.WaitingAt})
	}
	for _, h := range r.Held {
		if r.Spec == nil || h.Edge >= len(r.Spec.Edges) {
			continue
		}
		e := r.Spec.Edges[h.Edge]
		pick(runNeed{Run: r.ID, Title: title, Number: r.Number, Kind: needCap, Request: "cap-" + strconv.Itoa(h.Edge), Node: e.From, Edge: h.Edge, Text: capText(r, h.Edge), Since: h.At})
	}
	return best
}

func capText(r *Run, edge int) string {
	e := r.Spec.Edges[edge]
	limit := e.MaxFires + r.Extra[edge]
	return fmt.Sprintf("%s → %s ran %d of %s", roleOf(r, e.From), roleOf(r, e.To), limit, rounds(limit))
}

func rounds(n int) string {
	if n == 1 {
		return "1 round"
	}
	return strconv.Itoa(n) + " rounds"
}

func pendingOf(p pendingStep) *pendingInfo {
	info := &pendingInfo{Command: p.spec.Shell, Dir: p.spec.Dir, Main: p.spec.Main}
	if p.spec.inMain() && !strings.ContainsAny(info.Command, "*()") {
		info.Rule = bashTool + "(" + info.Command + ")"
	}
	return info
}

func (a *app) feedNeeds(ctx context.Context, workspace string) {
	needs := []runNeed{}
	a.mu.Lock()
	for _, r := range a.runs {
		if r.Workspace != workspace {
			continue
		}
		if n := r.need(); n != nil {
			needs = append(needs, *n)
		}
	}
	prev, sent := a.sentNeeds[workspace]
	sent = sent || a.resync
	a.mu.Unlock()
	sortNeeds(needs)
	if sent && prev != nil && reflect.DeepEqual(prev, needs) || !sent && len(needs) == 0 {
		return
	}
	a.needs(ctx, workspace, needs)
	a.mu.Lock()
	a.sentNeeds[workspace] = needs
	a.mu.Unlock()
}

func (a *app) readyAgain() {
	a.mu.Lock()
	a.sentNeeds = map[string][]runNeed{}
	a.mu.Unlock()
}

func sortNeeds(needs []runNeed) {
	slices.SortFunc(needs, func(x, y runNeed) int { return cmp.Or(x.Since.Compare(y.Since), cmp.Compare(x.Run, y.Run)) })
}
