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
	baseDefault    = "default"
	baseNoteTitle  = "Started from the local default branch"
)

var (
	errHome       = errors.New("Agents aren't available in the home workspace")
	errNoFolder   = errors.New("the workspace has no folders")
	errNotGit     = errors.New("the worktree is not in a git folder of the workspace")
	errBadBranch  = errors.New("not a valid branch name")
	errChildLimit = fmt.Errorf("a session may run at most %d child sessions at once", maxChildren)
	errRepoFolder = errors.New("this repo is not a folder of the workspace; connect it first")
	errRepoName   = errors.New("a repo child needs a slug or a branch")
	errRepoOnly   = errors.New("slug and base go with repo")
	errRepoBase   = errors.New("base is default or empty")
)

type worktreeNewArgs struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	From   string `json:"from"`
	Switch bool   `json:"switch"`
	Slug   string `json:"slug,omitempty"`
	Base   string `json:"base,omitempty"`
}

type worktreeOp struct {
	ID    string `json:"id"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
	Path  string `json:"path"`
	Note  string `json:"note"`
}

func (a *app) newSession(ctx context.Context, h *pluginsdk.Host, workspaceID, from string, args newArgs) (newResult, error) {
	agent := cmp.Or(args.Agent, agentClaude)
	if agent != agentClaude {
		return newResult{}, fmt.Errorf("%w: %s", errUnknownAgent, agent)
	}
	p, root, err := workspaceRoot(ctx, h, workspaceID)
	if err != nil {
		return newResult{}, err
	}
	if args.Finish != "" && !validFinish(args.Finish) {
		return newResult{}, fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, args.Finish)
	}
	rec := record{Workspace: workspaceID, Agent: agent, Title: titleOf(args.Title), TitleSet: args.Title != "", Notify: storedNotify(args.Notify), DisallowedTools: args.DisallowedTools, SystemPrompt: args.SystemPrompt, StartMode: args.Mode, FirstMode: args.FirstMode, StartModel: args.Model, CronJob: args.CronJob, Trigger: args.Trigger}
	if args.Run != "" {
		rec.Run = &api.Run{ID: args.Run, Title: args.RunTitle, After: args.RunAfter}
	}
	if parent, err := a.lookup(workspaceID, from); err == nil {
		if prec := parent.snapshot(); !prec.restricted() {
			rec.ParentSession, rec.Advisor, root = prec.ID, a.sessionAdvisor(ctx, prec), prec.Worktree
		}
	}
	var plan worktreeNewArgs
	var ready *setup
	switch {
	case args.Repo != "":
		if plan, root, err = planRepoWorktree(ctx, p, args); err != nil {
			return newResult{}, err
		}
		rec.RepoPath = plan.Repo
		ready = newSetup(plan)
	case args.Slug != "" || args.Base != "":
		return newResult{}, errRepoOnly
	case args.Branch != "":
		if plan, root, err = planWorktree(ctx, p, root, args.Branch); err != nil {
			return newResult{}, err
		}
		ready = newSetup(plan)
	}
	if args.worktree != "" {
		if err := workspaceWorktree(ctx, p, args.worktree); err != nil {
			return newResult{}, err
		}
		root = args.worktree
	}
	rec.Worktree = root
	rec.Finish, rec.AFK = a.sessionDefaults(ctx, rec.ParentSession)
	rec.Finish = cmp.Or(args.Finish, rec.Finish)
	if args.AFK != nil {
		rec.AFK = *args.AFK
	}
	s, err := a.spawn(rec, promptArgs{Text: args.Prompt, From: from, Review: args.review}, ready)
	if ready != nil && s != nil {
		go a.createWorktree(s, ready)
	}
	if err != nil {
		return newResult{}, err
	}
	if rec.RepoPath != "" {
		go a.landingChanged()
	}
	return newResult{Session: s.rec.ID, Worktree: root, parent: rec.ParentSession}, nil
}

func workspaceRoot(ctx context.Context, h *pluginsdk.Host, workspaceID string) (pluginsdk.Workspace, string, error) {
	p, err := h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return pluginsdk.Workspace{}, "", err
	}
	if p.Home {
		return pluginsdk.Workspace{}, "", errHome
	}
	root, _, err := workRoot(p)
	return p, root, err
}

func workRoot(p pluginsdk.Workspace) (string, []string, error) {
	roots := p.Roots()
	if len(roots) == 0 {
		return "", nil, errNoFolder
	}
	i := slices.IndexFunc(roots, func(r string) bool {
		_, ok := gitrepo.RepoRoot(r)
		return ok
	})
	i = max(i, 0)
	root := roots[i]
	return root, slices.Delete(roots, i, i+1), nil
}

func (a *app) spawn(rec record, first promptArgs, ready *setup) (*session, error) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if err := a.canSpawnLocked(rec.ParentSession); err != nil {
		return nil, err
	}
	if a.leavingLocked(rec.Worktree) {
		return nil, errFolderLeaving
	}
	return a.launch(rec, first, ready)
}

func (a *app) canSpawnLocked(parent string) error {
	if parent == "" {
		return nil
	}
	if p := a.session(parent); p == nil || p.snapshot().closed() {
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

func planWorktree(ctx context.Context, p pluginsdk.Workspace, base, branch string) (worktreeNewArgs, string, error) {
	if !gitrepo.ValidBranchName(branch) {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errBadBranch, branch)
	}
	top, ok := gitrepo.RepoRoot(base)
	if !ok {
		return worktreeNewArgs{}, "", errNotGit
	}
	for _, repo := range p.Paths() {
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

func planRepoWorktree(ctx context.Context, p pluginsdk.Workspace, args newArgs) (worktreeNewArgs, string, error) {
	repo, ok := folderIn(p, args.Repo)
	if !ok {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errRepoFolder, args.Repo)
	}
	if args.Slug == "" && args.Branch == "" {
		return worktreeNewArgs{}, "", errRepoName
	}
	if args.Base != "" && args.Base != baseDefault {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errRepoBase, args.Base)
	}
	branch := cmp.Or(args.Branch, taskBranchPrefix+args.Slug)
	if !gitrepo.ValidBranchName(branch) {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errBadBranch, branch)
	}
	f, err := gitrepo.OpenFolder(ctx, repo)
	if err != nil {
		return worktreeNewArgs{}, "", errNotGit
	}
	dir := filepath.Join(f.Trees[0].Path, worktreesDir, cmp.Or(args.Slug, branch), f.Rel)
	return worktreeNewArgs{Repo: repo, Branch: branch, Slug: args.Slug, Base: baseDefault}, dir, nil
}

func workspaceWorktree(ctx context.Context, p pluginsdk.Workspace, dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return errNotGit
	}
	if top, ok := gitrepo.RepoRoot(dir); !ok || top != dir {
		return errNotGit
	}
	for _, repo := range p.Paths() {
		if f, err := gitrepo.OpenFolder(ctx, repo); err == nil {
			if _, ok := f.Member(dir); ok {
				return nil
			}
		}
	}
	return errNotGit
}

func (a *app) createWorktree(s *session, ready *setup) {
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), worktreeLimit)
	defer cancel()
	note, err := a.newWorktree(ctx, s.snapshot().Workspace, ready.args)
	if err == nil {
		a.setFork(ctx, s)
	}
	if note != "" && ready.args.Base != "" {
		s.notice(baseNoteTitle, note)
	}
	ready.finish(err)
}

func (a *app) newWorktree(ctx context.Context, workspaceID string, args worktreeNewArgs) (string, error) {
	var op worktreeOp
	if err := a.host.RunCommand(ctx, worktreeNewCmd, workspaceID, args, &op); err != nil {
		return "", fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	note, err := a.awaitOp(ctx, workspaceID, op.ID)
	if err != nil {
		return "", fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	return note, nil
}
