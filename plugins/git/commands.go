package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
		removePromptCommand:      worktreeRemovePromptCommand,
		"git.repo-facts":         repoFactsCommand,
		"git.clone":              cloneCommand,
		"git.worktree-index":     worktreeIndexCommand,
		"git.worktree-state":     worktreeStateCommand,
		"git.branch-delete":      branchDeleteCommand,
		"git.worktree-unpushed":  worktreeUnpushedCommand,
		"git.repos-in":           reposInCommand,
	}
)

type commandFunc func(ctx context.Context, a *app, c call) (any, error)

type call struct {
	h           *pluginsdk.Host
	workspaceID string
	state       *state
	args        worktreeArgs
	ui          bool
	via         string
}

type worktreeArgs struct {
	ID      string   `json:"id"`
	Repo    string   `json:"repo"`
	Path    string   `json:"path"`
	Branch  string   `json:"branch"`
	Force   bool     `json:"force"`
	Discard bool     `json:"discard"`
	Left    bool     `json:"left"`
	From    string   `json:"from"`
	Switch  *bool    `json:"switch"`
	Slug    string   `json:"slug"`
	Base    string   `json:"base"`
	URL     string   `json:"url"`
	Into    string   `json:"into"`
	Paths   []string `json:"paths"`
	Proof   bool     `json:"proof"`
	Tip     string   `json:"tip"`
	Dates   bool     `json:"dates"`

	SeenDirty   int `json:"seenDirty"`
	SeenSecrets int `json:"seenSecrets"`
	SeenOrphans int `json:"seenOrphans"`
}

type worktreeInfo struct {
	Repo      string    `json:"repo"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Head      string    `json:"head"`
	Committed time.Time `json:"committed"`
	Main      bool      `json:"main"`
	Current   bool      `json:"current"`
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

type prompt struct {
	Title   string     `json:"title"`
	Detail  string     `json:"detail,omitempty"`
	Command string     `json:"command"`
	Args    removeArgs `json:"args"`
}

type removeArgs struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Force       bool   `json:"force,omitempty"`
	SeenSecrets int    `json:"seenSecrets,omitempty"`
	SeenOrphans int    `json:"seenOrphans,omitempty"`
}

type opStatus struct {
	Done   bool   `json:"done"`
	Error  string `json:"error,omitempty"`
	Path   string `json:"path,omitempty"`
	Notice string `json:"notice,omitempty"`
	Note   string `json:"note,omitempty"`
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	s := a.get(cmd.WorkspaceID)
	if err := a.sync(ctx, h, cmd.WorkspaceID, s); err != nil {
		return nil, err
	}
	run, ok := commandTable[cmd.ID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
	}
	c := call{h: h, workspaceID: cmd.WorkspaceID, state: s, ui: !cmd.Agent && cmd.Via == "", via: cmd.Via}
	if len(cmd.Args) > 0 {
		if err := json.Unmarshal(cmd.Args, &c.args); err != nil {
			return nil, fmt.Errorf("%s args: %w", cmd.ID, err)
		}
	}
	return run(ctx, a, c)
}

func worktreesCommand(ctx context.Context, _ *app, c call) (any, error) {
	return listWorktrees(ctx, c.h, c.workspaceID)
}

func worktreeSwitchCommand(ctx context.Context, _ *app, c call) (any, error) {
	return nil, switchWorktree(ctx, c.h, c.workspaceID, c.state, c.args.Repo, c.args.Path)
}

func branchesCommand(ctx context.Context, _ *app, c call) (any, error) {
	return listBranches(ctx, c.h, c.workspaceID)
}

func branchSwitchCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startOp(ctx, c, prepareBranchSwitch)
}

func worktreeNewCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startOp(ctx, c, func(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs) (opFunc, error) {
		run, err := prepareNew(ctx, h, workspaceID, args)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, report *opReport) (string, error) {
			path, err := run(ctx, report)
			if err == nil {
				logError("record "+path, a.meta.update(ctx, h, path, func(m *worktreeMeta) { m.MadeBy = c.via }))
			}
			return path, err
		}, nil
	})
}

func worktreeRemoveCommand(ctx context.Context, a *app, c call) (any, error) {
	if err := c.costRefused(); err != nil {
		return nil, err
	}
	if !c.args.seesCost() && !c.ui && (c.args.SeenSecrets > 0 || c.args.SeenOrphans > 0) {
		return nil, errSeenRefused
	}
	return a.startOp(ctx, c, prepareRemove)
}

func opStatusCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.ops.status(ctx, c.workspaceID, c.args.ID)
}

func worktreeSessionsCommand(ctx context.Context, _ *app, c call) (any, error) {
	return worktreeSessions(ctx, c.h, c.workspaceID, c.args.Path)
}

func worktreeRemovePromptCommand(ctx context.Context, _ *app, c call) (any, error) {
	if err := c.costRefused(); err != nil {
		return nil, err
	}
	return removePrompt(ctx, c.h, c.workspaceID, c.args)
}

func (c call) costRefused() error {
	if c.args.seesCost() && c.via != discardPlugin {
		return errDiscardRefused
	}
	return nil
}

func repoFactsCommand(ctx context.Context, _ *app, c call) (any, error) {
	return readRepoFacts(ctx, c.h, c.workspaceID, c.args.Path)
}

func cloneCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.startClone(ctx, c)
}

func worktreeIndexCommand(ctx context.Context, a *app, c call) (any, error) {
	return a.worktreeIndex(ctx, c)
}

func worktreeStateCommand(ctx context.Context, _ *app, c call) (any, error) {
	_, known := c.state.indexCopy()
	return worktreeStates(ctx, c.h, c.workspaceID, c.args, known)
}

func worktreeUnpushedCommand(ctx context.Context, _ *app, c call) (any, error) {
	return unpushedWorktrees(ctx, c.h, c.workspaceID)
}

func branchDeleteCommand(ctx context.Context, _ *app, c call) (any, error) {
	if c.via != discardPlugin {
		return nil, errBranchDeleteRefused
	}
	return nil, deleteBranch(ctx, c.h, c.workspaceID, c.args)
}
