package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	graphsPlugin = "graphs"
	kindRun      = "run"
	needStep     = "step"
	toolExecute  = "execute"
)

type runNeed struct {
	Run     string    `json:"run"`
	Title   string    `json:"title"`
	Kind    string    `json:"kind"`
	Request string    `json:"request"`
	Node    string    `json:"node"`
	Edge    int       `json:"edge,omitempty"`
	Text    string    `json:"text"`
	Command string    `json:"command,omitempty"`
	Since   time.Time `json:"since"`
}

type runNeedsArgs struct {
	Runs []runNeed `json:"runs"`
}

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

func (a *app) runNeedsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args runNeedsArgs) (any, error) {
	if cmd.Agent || cmd.Via != graphsPlugin {
		return nil, fmt.Errorf("%w: %s", errUnknownCmd, cmd.ID)
	}
	prev := a.runNeeds.set(cmd.WorkspaceID, args.Runs)
	for _, c := range runNeedChanges(cmd.WorkspaceID, prev, args.Runs) {
		if a.host == nil {
			break
		}
		if _, err := a.host.Publish(ctx, api.TopicChanged, c); err != nil {
			fmt.Fprintln(os.Stderr, "agents: publish:", err)
		}
	}
	if a.host != nil {
		a.workspaceState(cmd.WorkspaceID).throttle.kick()
	}
	return nil, nil
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
		summary.Kind, summary.Text, summary.Tool = entryPermission, n.Command, toolExecute
	}
	return sessionInfo{
		record:          record{ID: n.Run, Workspace: workspace, Title: n.Title, Status: statusWaiting, Run: &api.Run{ID: n.Run, Title: n.Title}, CreatedAt: since, UpdatedAt: since},
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
