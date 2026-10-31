package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"clubmate/pkg/pluginsdk"
)

var (
	errUnknownCommand = errors.New("unknown command")
	commandTable      = map[string]commandFunc{
		"git.worktrees":          worktreesCommand,
		"git.worktree-switch":    worktreeSwitchCommand,
		"git.branches":           branchesCommand,
		"git.branch-switch":      branchSwitchCommand,
		"git.worktree-new":       worktreeNewCommand,
		"git.worktree-remove":    worktreeRemoveCommand,
		"git.worktree-op-status": opStatusCommand,
		"git.worktree-sessions":  worktreeSessionsCommand,
	}
)

type commandFunc func(ctx context.Context, a *app, c call) (any, error)

type call struct {
	h         *pluginsdk.Host
	projectID string
	state     *state
	args      worktreeArgs
}

type worktreeArgs struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Force  bool   `json:"force"`
	From   string `json:"from"`
	Switch *bool  `json:"switch"`
}

type worktreeInfo struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Main    bool   `json:"main"`
	Current bool   `json:"current"`
}

type branchInfo struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Remote bool   `json:"remote,omitempty"`
}

type sessionInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type opStarted struct {
	ID string `json:"id"`
}

type opStatus struct {
	Done   bool   `json:"done"`
	Error  string `json:"error,omitempty"`
	Path   string `json:"path,omitempty"`
	Notice string `json:"notice,omitempty"`
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	s := a.get(cmd.ProjectID)
	if err := a.sync(ctx, h, cmd.ProjectID, s); err != nil {
		return nil, err
	}
	run, ok := commandTable[cmd.ID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
	}
	c := call{h: h, projectID: cmd.ProjectID, state: s}
	if len(cmd.Args) > 0 {
		if err := json.Unmarshal(cmd.Args, &c.args); err != nil {
			return nil, fmt.Errorf("%s args: %w", cmd.ID, err)
		}
	}
	return run(ctx, a, c)
}

func worktreesCommand(ctx context.Context, _ *app, c call) (any, error) {
	return listWorktrees(ctx, c.h, c.projectID)
}

func worktreeSwitchCommand(ctx context.Context, _ *app, c call) (any, error) {
	return nil, switchWorktree(ctx, c.h, c.projectID, c.state, c.args.Repo, c.args.Path)
}

func branchesCommand(ctx context.Context, _ *app, c call) (any, error) {
	return listBranches(ctx, c.h, c.projectID)
}

func branchSwitchCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startOp(ctx, c, prepareBranchSwitch)
}

func worktreeNewCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startOp(ctx, c, prepareNew)
}

func worktreeRemoveCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startOp(ctx, c, prepareRemove)
}

func opStatusCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.ops.status(ctx, c.projectID, c.args.ID)
}

func worktreeSessionsCommand(ctx context.Context, _ *app, c call) (any, error) {
	return worktreeSessions(ctx, c.h, c.projectID, c.args.Path)
}
