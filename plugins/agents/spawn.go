package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/gitrepo"
)

const (
	maxChildren    = 8
	worktreeLimit  = 10 * time.Minute
	worktreeNewCmd = "git.worktree-new"
	worktreeOpCmd  = "git.worktree-op-status"
	worktreesDir   = ".worktrees"
)

var (
	errHome       = errors.New("Agents aren't available in the home workspace")
	errNoFolder   = errors.New("the workspace has no folders")
	errNotGit     = errors.New("the worktree is not in a git folder of the workspace")
	errBadBranch  = errors.New("not a valid branch name")
	errChildLimit = fmt.Errorf("a session may run at most %d child sessions at once", maxChildren)
)

type worktreeNewArgs struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	From   string `json:"from"`
	Switch bool   `json:"switch"`
}

type worktreeOp struct {
	ID    string `json:"id"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
	Path  string `json:"path"`
}

func (a *app) newSession(ctx context.Context, h *pluginsdk.Host, projectID, from string, args newArgs) (newResult, error) {
	agent := cmp.Or(args.Agent, agentClaude)
	if agent != agentClaude {
		return newResult{}, fmt.Errorf("%w: %s", errUnknownAgent, agent)
	}
	p, root, err := projectRoot(ctx, h, projectID)
	if err != nil {
		return newResult{}, err
	}
	rec := record{Workspace: projectID, Agent: agent, Title: titleOf(args.Title), TitleSet: args.Title != "", Notify: storedNotify(args.Notify)}
	if parent, err := a.lookup(projectID, from); err == nil {
		prec := parent.snapshot()
		rec.ParentSession, rec.Advisor, root = prec.ID, a.sessionAdvisor(ctx, prec), prec.Worktree
	}
	var plan worktreeNewArgs
	var ready *setup
	if args.Branch != "" {
		if plan, root, err = planWorktree(ctx, p, root, args.Branch); err != nil {
			return newResult{}, err
		}
		ready = newSetup(plan)
	}
	rec.Worktree = root
	rec.Finish, rec.AFK = a.sessionDefaults(ctx, rec.ParentSession)
	s, err := a.spawn(rec, promptArgs{Text: args.Prompt, From: from}, ready)
	if ready != nil && s != nil {
		go a.createWorktree(s, ready)
	}
	if err != nil {
		return newResult{}, err
	}
	return newResult{Session: s.rec.ID, Worktree: root, parent: rec.ParentSession}, nil
}

func projectRoot(ctx context.Context, h *pluginsdk.Host, projectID string) (pluginsdk.Project, string, error) {
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return pluginsdk.Project{}, "", err
	}
	if p.Home {
		return pluginsdk.Project{}, "", errHome
	}
	root, _, err := workRoot(p)
	return p, root, err
}

func workRoot(p pluginsdk.Project) (string, []string, error) {
	if len(p.Roots) == 0 {
		return "", nil, errNoFolder
	}
	i := slices.IndexFunc(p.Roots, func(r string) bool {
		_, ok := gitrepo.RepoRoot(r)
		return ok
	})
	i = max(i, 0)
	return p.Roots[i], slices.Delete(slices.Clone(p.Roots), i, i+1), nil
}

func (a *app) spawn(rec record, first promptArgs, ready *setup) (*session, error) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if err := a.canSpawnLocked(rec.ParentSession); err != nil {
		return nil, err
	}
	return a.launch(rec, first, ready)
}

func (a *app) canSpawnLocked(parent string) error {
	if parent == "" {
		return nil
	}
	if p := a.session(parent); p == nil || p.snapshot().Closed {
		return errClosed
	}
	if a.runningChildren(parent) >= maxChildren {
		return errChildLimit
	}
	return nil
}

func (a *app) launch(rec record, first promptArgs, ready *setup) (*session, error) {
	s := a.createSession(rec, ready)
	if first.Text == "" {
		return s, nil
	}
	first.Session = s.rec.ID
	return s, a.submit(s, &first)
}

func (a *app) createSession(rec record, ready *setup) *session {
	now := time.Now().UTC()
	rec.ID, rec.Status, rec.CreatedAt, rec.UpdatedAt = id.New(api.IDPrefix), statusIdle, now, now
	s := a.addSession(rec, true, ready)
	a.notes.mark(s, changeCreated, true)
	a.kick(rec.Workspace)
	go a.attachInBackground(s)
	return s
}

func planWorktree(ctx context.Context, p pluginsdk.Project, base, branch string) (worktreeNewArgs, string, error) {
	if !gitrepo.ValidBranchName(branch) {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errBadBranch, branch)
	}
	top, ok := gitrepo.RepoRoot(base)
	if !ok {
		return worktreeNewArgs{}, "", errNotGit
	}
	for _, repo := range p.Repos {
		f, err := gitrepo.OpenFolder(ctx, repo)
		if err != nil {
			continue
		}
		if _, ok := f.Member(top); ok {
			dir := filepath.Join(f.Trees[0].Path, worktreesDir, branch, f.Rel)
			return worktreeNewArgs{Repo: repo, Branch: branch, From: top}, dir, nil
		}
	}
	return worktreeNewArgs{}, "", errNotGit
}

func (a *app) createWorktree(s *session, ready *setup) {
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), worktreeLimit)
	defer cancel()
	err := a.newWorktree(ctx, s.snapshot().Workspace, ready.args)
	if err == nil {
		a.setFork(ctx, s)
	}
	ready.finish(err)
}

func (a *app) newWorktree(ctx context.Context, projectID string, args worktreeNewArgs) error {
	var op worktreeOp
	if err := a.host.RunCommand(ctx, worktreeNewCmd, projectID, args, &op); err != nil {
		return fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	if err := a.awaitOp(ctx, projectID, op.ID); err != nil {
		return fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	return nil
}
