package automation

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
)

const (
	PromptCommand = "agents.prompt"
	OriginCronJob = "cronJob"
	OriginTrigger = "trigger"
	OwnerNone     = "none"
	nobody        = "nobody — failures go to the inbox"
)

const (
	sessionRunning = "running"
	sessionWaiting = "waiting"
	noApprover     = "no app to approve"
)

type Action struct {
	ID        string
	Name      string
	Workspace string
	Command   string
	Args      json.RawMessage
	Owner     string
	CreatedBy string
}

type Entry[P any] interface {
	comparable
	Keyed
	Action() Action
	Clone() P
}

type CommandRequest struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	Args        json.RawMessage `json:"args,omitempty"`
	Agent       bool            `json:"agent"`
	Caller      string          `json:"caller,omitempty"`
}

type IDArgs struct {
	ID string `json:"id"`
}

type Executor struct {
	Origin     string
	Sessions   func(ctx context.Context, workspace string) ([]api.Session, error)
	RunCommand func(ctx context.Context, req CommandRequest) (json.RawMessage, error)
	Log        func(error)
}

type promptTarget struct {
	Session string `json:"session"`
}

func (x Executor) Fire(ctx context.Context, act Action, r Run) Run {
	sessions, reason := x.Guard(ctx, act)
	if reason == "" {
		if _, busy := BusyTarget(sessions, act); busy {
			reason = ReasonBusy
		}
	}
	if reason != "" {
		r.Status, r.Reason = StatusSkipped, reason
		return r
	}
	return x.Execute(ctx, act, r)
}

func (x Executor) Guard(ctx context.Context, act Action) ([]api.Session, string) {
	if act.Owner == "" && act.Command != PromptCommand {
		return nil, ""
	}
	sessions, err := x.Sessions(ctx, act.Workspace)
	if err != nil {
		x.Log(err)
		if act.Owner != "" {
			return nil, ReasonUnknown
		}
	}
	if s, ok := agentcall.Find(sessions, act.Owner); act.Owner != "" && (!ok || s.Closed()) {
		return sessions, ReasonGone
	}
	return sessions, ""
}

func (x Executor) Execute(ctx context.Context, act Action, r Run) Run {
	args, err := x.withOrigin(act)
	if err != nil {
		r.Status, r.Reason = StatusError, err.Error()
		return r
	}
	if act.Command == PromptCommand {
		var target promptTarget
		_ = json.Unmarshal(act.Args, &target)
		r.Target = target.Session
	}
	raw, err := x.RunCommand(ctx, CommandRequest{ID: act.Command, WorkspaceID: act.Workspace, Args: args, Agent: true, Caller: act.Owner})
	switch {
	case err == nil:
		r.Status, r.Result = StatusOK, Truncate(string(raw), MaxResult)
	case strings.Contains(err.Error(), noApprover):
		r.Status, r.Reason = StatusSkipped, ReasonNoApp
	default:
		r.Status, r.Reason = StatusError, err.Error()
	}
	return r
}

func (x Executor) withOrigin(act Action) (json.RawMessage, error) {
	if x.Origin == "" || act.Command != agentNew && (act.Command != agentEmit || x.Origin != OriginCronJob) {
		return act.Args, nil
	}
	fields := map[string]json.RawMessage{}
	if len(act.Args) > 0 {
		if err := json.Unmarshal(act.Args, &fields); err != nil {
			return nil, fmt.Errorf("%s args: %w", act.Command, err)
		}
	}
	fields[x.Origin], _ = json.Marshal(act.ID)
	return json.Marshal(fields)
}

func BusyTarget(sessions []api.Session, act Action) (string, bool) {
	if act.Command != PromptCommand {
		return "", false
	}
	var target promptTarget
	if json.Unmarshal(act.Args, &target) != nil {
		return "", false
	}
	s, ok := agentcall.Find(sessions, target.Session)
	return target.Session, ok && (s.Status == sessionRunning || s.Status == sessionWaiting)
}

func Allowed(cmd pluginsdk.CommandRun, act Action, parents map[string]string) bool {
	if !cmd.Agent {
		return true
	}
	if cmd.Caller == "" {
		return false
	}
	return agentcall.Descends(act.Owner, cmd.Caller, parents) || agentcall.Descends(act.CreatedBy, cmd.Caller, parents)
}

func CallerOf(cmd pluginsdk.CommandRun) string {
	if cmd.Agent {
		return cmd.Caller
	}
	return ""
}

func Decode(cmd pluginsdk.CommandRun, v any) error {
	if len(cmd.Args) == 0 {
		return nil
	}
	if err := json.Unmarshal(cmd.Args, v); err != nil {
		return fmt.Errorf("%s args: %w", cmd.ID, err)
	}
	return nil
}

func Sort[P Entry[P]](items []P) {
	slices.SortFunc(items, func(x, y P) int {
		a, b := x.Action(), y.Action()
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
}

func OwnerLabel(owner string, titles map[string]string) string {
	if owner == "" {
		return nobody
	}
	return Names(owner, titles)
}

func CallCommand(ctx context.Context, h *pluginsdk.Host, req CommandRequest) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := h.Call(ctx, "command.run", req, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
