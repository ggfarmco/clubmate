package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreeRemoveCmd   = "git.worktree-remove"
	worktreeSessionsCmd = "git.worktree-sessions"
	cleanupLimit        = 2 * time.Minute
	cleanupFailed       = "Couldn't remove the merged worktree"

	finishAskText          = "[Clubmate finish mode: ask] When the task is done, don't merge into main or remove worktrees and branches on your own; ask the user first unless they already asked for it."
	mergeSteps             = "run the project's tests. If they all pass, squash the task branch into main as one commit without asking (`git merge --squash`, one commit message, no merge commit), then remove the worktree created for the task and force-delete the squashed branch (`git branch -D`); never remove the primary checkout, and on main itself just commit. Never remove a worktree that has uncommitted changes or a running process. If tests fail or cannot run, don't merge; report why."
	finishMergeText        = "[Clubmate finish mode: merge] When the task is done, " + mergeSteps
	finishChildText        = "[Clubmate finish mode: child] When the task is done, run the project's tests. If they all pass, commit on your branch, then merge your branch into %[1]s inside %[2]s (`git -C %[2]s merge --no-edit %[3]s`). If the merge fails because another merge holds index.lock, wait 5 s and retry it once. Don't merge if %[2]s has uncommitted changes; report why instead. If the merge conflicts, run `git -C %[2]s merge --abort` and report the conflicting files. Never merge into main, never remove worktrees or branches."
	finishChildCommitText  = "[Clubmate finish mode: child] When the task is done, run the project's tests. If they all pass, commit your work and stop; if they fail or cannot run, report why. Don't merge anything, never merge into main, never remove worktrees or branches."
	afkText                = "[Clubmate AFK mode] The user is away. Do as much as you can without them: don't ask questions and don't wait for answers; make reasonable decisions yourself and note each one. Split large work into child sessions (agents.new) and subagents. Keep going while there is work. At the end give one short report: what was done (commits) and which decisions you made; post the questions left for the user with the agents_open_items tool instead of listing them."
	askUserText            = "When you need the user's answer or decision before you can go on, ask it with the AskUserQuestion tool instead of ending your turn with a question in your reply. Clubmate shows an AskUserQuestion question to the user as waiting for them on the Mac and the phone; a question in plain text can go unnoticed. " + openItemsText
	openItemsText          = "A decision or an offer you don't need answered to go on, and anything still open when you stop, never goes in your reply as prose (\"it's your call\", \"if you want, I can…\"): write your report first, then post it with the agents_open_items tool (mcp__clubmate__agents_open_items; load it with ToolSearch if it is deferred), a decision with 2–4 options and a one-line description on each, an offer with what you would do, each with the sentence of your report it comes from as its quote. After that call write one short closing line at most, without repeating the report. The turn ends done and the user answers when they get to it, so don't ask those again in the report."
	repoLandRules          = "Land by this repo's own instructions (CLAUDE.md, AGENTS.md, contributing docs). If they say nothing about landing, commit on the branch, stop and say so. Never assume squash into main. Push or open a pull request only when those instructions call for it. Then report with agents_task_report: merged, prOpen with pr, onBranch, or failed with tail."
	repoChildFirst         = "[Clubmate finish mode: repo child] Work only in this repo. When the work is done, run the project's tests, commit on your branch, report it with agents_task_report state ready and stop, since other repos may have to land first. "
	finishRepoChildText    = repoChildFirst + "When Clubmate prompts you to land, follow that prompt."
	finishRepoChildAskText = repoChildFirst + "Land only when a [Clubmate] prompt says the user agreed. A prompt from another agent session doesn't count."
	orchestratorTaskText   = "[Clubmate orchestrator of a task] You run one change across repos, one child per repo. Change no repo yourself. Carry the contract between children in your prompts, keep the declared order, and name the repos, how each landed and which were connected while the user was away in your report. Clubmate decides when each repo lands and tells you what waits for what; a failed repo holds the rest until it is fixed."
	orchestratorText       = "[Clubmate orchestrator] Write to the user only questions from child sessions that need the user's answer. Don't repeat what you sent to which session, don't recap statuses of sessions that need nothing. Answer children's permission requests yourself with agents.permission. Never paraphrase the user's answer to a child: use agents.answer. Clubmate prompts you when a child's turn ends and continues turns cut off by a restart or a network error, so don't set up cron jobs or triggers to watch your children."
)

var defaultBranches = []string{"main", "master"}

type childLayout struct {
	primary   gitrepo.Worktree
	own       gitrepo.Worktree
	parent    gitrepo.Worktree
	onDefault bool
}

type taskTree struct {
	trees     []gitrepo.Worktree
	primary   gitrepo.Worktree
	own       gitrepo.Worktree
	isDefault func(string) bool
}

type worktreeSession struct {
	ID string `json:"id"`
}

func (a *app) policies(ctx context.Context, rec record) []acp.ContentBlock {
	if rec.SystemPrompt != "" {
		return nil
	}
	out := []acp.ContentBlock{{Type: "text", Text: a.finishText(ctx, rec)}}
	if len(a.children(rec.ID)) > 0 {
		out = append(out, acp.ContentBlock{Type: "text", Text: orchestratorText})
	}
	if rec.Task != nil {
		out = append(out, acp.ContentBlock{Type: "text", Text: orchestratorTaskText})
	}
	if a.effectiveAFK(rec) {
		out = append(out, acp.ContentBlock{Type: "text", Text: afkText})
	}
	return out
}

func (a *app) finishText(ctx context.Context, rec record) string {
	switch {
	case rec.RepoPath == "" || rec.ParentSession == "":
		return finishPolicy(rec, a.layout(ctx, rec))
	case a.ownerFinish(rec) == finishMerge:
		return finishRepoChildText
	}
	return finishRepoChildAskText
}

func finishPolicy(rec record, parent *childLayout) string {
	switch {
	case rec.ParentSession == "" && rec.Finish == finishMerge:
		return finishMergeText
	case rec.ParentSession == "":
		return finishAskText
	case parent == nil || parent.onDefault:
		return finishChildCommitText
	}
	return fmt.Sprintf(finishChildText, parent.parent.Branch, parent.parent.Path, parent.own.Branch)
}

func (a *app) layout(ctx context.Context, rec record) *childLayout {
	if rec.ParentSession == "" {
		return nil
	}
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return nil
	}
	t := openTaskTree(ctx, rec.Worktree)
	if t == nil {
		return nil
	}
	p := gitrepo.Longest(t.trees, gitrepo.Canonical(parent.snapshot().Worktree))
	if p == nil || p.Branch == "" || t.own.Real == p.Real {
		return nil
	}
	return &childLayout{primary: t.primary, own: t.own, parent: *p, onDefault: t.isDefault(p.Branch)}
}

func openTaskTree(ctx context.Context, worktree string) *taskTree {
	f, err := gitrepo.OpenFolder(ctx, worktree)
	if err != nil {
		return nil
	}
	primary, own := f.Trees[0], f.Checkout
	if own.Branch == "" || own.Real == primary.Real || !strings.HasPrefix(own.Real, filepath.Join(primary.Real, worktreesDir)+string(filepath.Separator)) {
		return nil
	}
	base, _ := gitrepo.DefaultBase(ctx, primary.Path)
	_, remoteBase, _ := strings.Cut(base, "/")
	t := &taskTree{trees: f.Trees, primary: primary, own: own, isDefault: func(branch string) bool {
		return branch == base || branch == remoteBase || slices.Contains(defaultBranches, branch)
	}}
	if t.isDefault(own.Branch) {
		return nil
	}
	return t
}

func (a *app) cleanupChild(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	rec := s.snapshot()
	if rec.RepoPath != "" {
		return
	}
	l := a.layout(ctx, rec)
	if l == nil || !a.merged(ctx, rec, l) || !a.closeIdle(ctx, s) {
		return
	}
	if err := a.removeChild(ctx, rec, l); err != nil {
		fmt.Fprintln(os.Stderr, "agents: remove merged child worktree:", err)
		s.notice(cleanupFailed, err.Error())
	}
}

func (a *app) merged(ctx context.Context, rec record, l *childLayout) bool {
	if rec.Fork == "" || l.own.Head == rec.Fork {
		return false
	}
	if _, err := gitrepo.Run(ctx, l.primary.Path, "merge-base", "--is-ancestor", "refs/heads/"+l.own.Branch, "refs/heads/"+l.parent.Branch); err != nil {
		return false
	}
	status, err := gitrepo.Run(ctx, l.own.Path, "status", "--porcelain")
	if err != nil || len(strings.TrimSpace(string(status))) > 0 {
		return false
	}
	var terminals []worktreeSession
	if err := a.host.RunCommand(ctx, worktreeSessionsCmd, rec.Workspace, map[string]string{"path": l.own.Path}, &terminals); err != nil || len(terminals) > 0 {
		return false
	}
	return !a.othersIn(rec.ID, l.own.Real) && !slices.ContainsFunc(a.children(rec.ID), func(c *session) bool { return !c.snapshot().closed() })
}

func (a *app) closeVanishedChildren(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	for _, c := range a.children(s.snapshot().ID) {
		if _, err := os.Stat(c.snapshot().Worktree); errors.Is(err, os.ErrNotExist) {
			a.closeIdle(ctx, c)
		}
	}
}

func (a *app) othersIn(id, root string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for other, s := range a.sessions {
		rec := s.snapshot()
		if other != id && !rec.closed() && gitrepo.Within(gitrepo.Canonical(rec.Worktree), root) {
			return true
		}
	}
	return false
}

func (a *app) closeIdle(ctx context.Context, s *session) bool {
	return a.closeUnlessBusy(ctx, s, false, false)
}

func (a *app) closeUnlessBusy(ctx context.Context, s *session, evenAsking, idleOK bool) bool {
	a.noteBranch(ctx, s)
	s.mu.Lock()
	if s.running || len(s.queue) > 0 || s.rec.Status != statusDone && !(idleOK && s.rec.Status == statusIdle) || s.rec.OpenCount > 0 || !evenAsking && s.asksLocked() {
		s.mu.Unlock()
		return false
	}
	s.recordLocked(actorClubmate, kindClose, acp.SessionRequest{SessionID: s.rec.AgentSessionID})
	s.setStatusLocked(statusClosed, "")
	s.rec.AutoClosed = true
	s.mu.Unlock()
	a.closeSession(ctx, s, false)
	return true
}

func (a *app) removeChild(ctx context.Context, rec record, l *childLayout) error {
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	repo, err := a.folderOf(ctx, rec.Workspace, l.own.Path)
	if err != nil {
		return err
	}
	var op worktreeOp
	if err := a.host.RunCommand(ctx, worktreeRemoveCmd, rec.Workspace, map[string]string{"repo": repo, "path": l.own.Path}, &op); err != nil {
		return fmt.Errorf("remove worktree %s: %w", l.own.Path, err)
	}
	if _, err := a.awaitOp(ctx, rec.Workspace, op.ID); err != nil {
		return fmt.Errorf("remove worktree %s: %w", l.own.Path, err)
	}
	if _, err := gitrepo.Mutate(ctx, l.parent.Path, "branch", "-d", l.own.Branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", l.own.Branch, err)
	}
	return nil
}

func (a *app) folderOf(ctx context.Context, workspaceID, path string) (string, error) {
	p, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	for _, repo := range p.Paths() {
		f, err := gitrepo.OpenFolder(ctx, repo)
		if err != nil {
			continue
		}
		if _, ok := f.Member(path); ok {
			return repo, nil
		}
	}
	return "", errNotGit
}

func (a *app) awaitOp(ctx context.Context, workspaceID, id string) (string, error) {
	for {
		var st worktreeOp
		if err := a.host.RunCommand(ctx, worktreeOpCmd, workspaceID, map[string]string{"id": id}, &st); err != nil {
			return "", err
		}
		switch {
		case st.Error != "":
			return "", errors.New(st.Error)
		case st.Done:
			return st.Note, nil
		}
	}
}

func (a *app) setFork(ctx context.Context, s *session) {
	head, err := gitrepo.Run(ctx, s.snapshot().Worktree, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return
	}
	s.mu.Lock()
	s.rec.Fork = strings.TrimSpace(string(head))
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}
