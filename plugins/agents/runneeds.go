package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"

	"clubmate/plugins/agents/api"
)

const (
	kindRun     = "run"
	needStep    = "step"
	toolExecute = "execute"
)

type runNeed = api.RunNeed

type runNeeds struct {
	mu   sync.Mutex
	byWS map[string][]runNeed
}

func (n *runNeeds) set(workspace string, needs []runNeed) []runNeed {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.byWS == nil {
		n.byWS = map[string][]runNeed{}
	}
	prev := n.byWS[workspace]
	n.byWS[workspace] = slices.Clone(needs)
	return prev
}

func (n *runNeeds) list(workspace string) []runNeed {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.byWS[workspace])
}

func (n *runNeeds) byRun(workspace string) map[string]runNeed {
	out := map[string]runNeed{}
	for _, need := range n.list(workspace) {
		out[need.Run] = need
	}
	return out
}

func (a *app) setRunNeeds(ctx context.Context, workspace string, needs []runNeed) {
	prev := a.runNeeds.set(workspace, needs)
	for _, c := range runNeedChanges(workspace, prev, needs) {
		if a.host == nil {
			break
		}
		if _, err := a.host.Publish(ctx, api.TopicChanged, c); err != nil {
			fmt.Fprintln(os.Stderr, "agents: publish:", err)
		}
	}
	if a.host != nil {
		a.workspaceState(workspace).throttle.kick()
	}
}

func runNeedChanges(workspace string, prev, next []runNeed) []api.Changed {
	var out []api.Changed
	for _, p := range prev {
		if !slices.ContainsFunc(next, func(n runNeed) bool { return n.Run == p.Run && n.Request == p.Request }) {
			out = append(out, api.Changed{Workspace: workspace, Session: p.Run, Kind: api.ChangeResolved, Request: p.Request, RequestKind: api.RequestPermission})
		}
	}
	for _, n := range next {
		if !slices.Contains(prev, n) {
			out = append(out, api.Changed{Workspace: workspace, Session: n.Run, Kind: api.ChangeStatus, Status: statusWaiting, Request: n.Request, RequestKind: api.RequestPermission})
		}
	}
	return out
}

func runInfo(workspace string, n runNeed) sessionInfo {
	since := n.Since
	summary := &pendingSummary{Kind: pendingQuestion, Text: n.Text, At: since}
	if n.Kind == needStep {
		summary.Kind, summary.Text, summary.Tool, summary.Role = entryPermission, n.Command, toolExecute, n.Role
	}
	return sessionInfo{
		record:          record{ID: n.Run, Workspace: workspace, Title: n.Title, Status: statusWaiting, Run: &api.Run{ID: n.Run, Title: n.Title, Number: n.Number}, CreatedAt: since, UpdatedAt: since},
		Kind:            kindRun,
		Pending:         []string{n.Request},
		PendingSummary:  summary,
		NeedsYou:        true,
		NeedsYouSince:   &since,
		Group:           groupNeedsYou,
		NotifyEffective: notifyAll,
	}
}

func runID(rec record) string {
	if rec.Run == nil {
		return ""
	}
	return rec.Run.ID
}
