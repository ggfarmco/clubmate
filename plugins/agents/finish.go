package main

import (
	"cmp"
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

	finishAskText         = "[Clubmate finish mode: ask] When the task is done, don't merge into main or remove worktrees and branches on your own; ask the user first unless they already asked for it."
	mergeSteps            = "run the project's tests. If they all pass, squash the task branch into main as one commit without asking (`git merge --squash`, one commit message, no merge commit), then remove the worktree created for the task and force-delete the squashed branch (`git branch -D`); never remove the primary checkout, and on main itself just commit. Never remove a worktree that has uncommitted changes or a running process. If tests fail or cannot run, don't merge; report why."
	finishMergeText       = "[Clubmate finish mode: merge] When the task is done, " + mergeSteps
	mergeNowText          = "Merge this task into main now: commit your remaining work on the task branch, then " + mergeSteps
	finishChildText       = "[Clubmate finish mode: child] When the task is done, run the project's tests. If they all pass, commit on your branch, then merge your branch into %[1]s inside %[2]s (`git -C %[2]s merge --no-edit %[3]s`). If the merge fails because another merge holds index.lock, wait 5 s and retry it once. Don't merge if %[2]s has uncommitted changes; report why instead. If the merge conflicts, run `git -C %[2]s merge --abort` and report the conflicting files. Never merge into main, never remove worktrees or branches."
	finishChildCommitText = "[Clubmate finish mode: child] When the task is done, run the project's tests. If they all pass, commit your work and stop; if they fail or cannot run, report why. Don't merge anything, never merge into main, never remove worktrees or branches."
	afkText               = "[Clubmate AFK mode] The user is away. Do as much as you can without them: don't ask questions and don't wait for answers; make reasonable decisions yourself and note each one. Split large work into child sessions (agents.new) and subagents. Keep going while there is work. At the end give one short report: what was done (commits), which decisions you made, and which questions are left for the user."
	orchestratorText      = "[Clubmate orchestrator] Write to the user only questions from child sessions that need the user's answer. Don't repeat what you sent to which session, don't recap statuses of sessions that need nothing. Answer children's permission requests yourself with agents.permission. Never paraphrase the user's answer to a child: use agents.answer."
)

var (
	errNotMergeable = errors.New("only an idle session with changes in its own task worktree can merge to main")
	errMergeBusy    = errors.New("another agent session works in this worktree")
	errMainDirty    = errors.New("the main checkout has uncommitted changes")
	defaultBranches = []string{"main", "master"}
)

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
	out := []acp.ContentBlock{{Type: "text", Text: finishPolicy(rec, a.layout(ctx, rec))}}
	if len(a.children(rec.ID)) > 0 {
		out = append(out, acp.ContentBlock{Type: "text", Text: orchestratorText})
	}
	if a.effectiveAFK(rec) {
		out = append(out, acp.ContentBlock{Type: "text", Text: afkText})
	}
	return out
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

func mergeable(rec record, changes bool) bool {
	inTask := strings.Contains(filepath.Clean(rec.Worktree)+string(filepath.Separator), string(filepath.Separator)+worktreesDir+string(filepath.Separator))
	return changes && inTask && rec.ParentSession == "" && rec.Status == statusDone && cmp.Or(rec.Finish, finishAsk) == finishAsk
}

func (a *app) mergeToMain(ctx context.Context, projectID string, s *session) error {
	rec := s.snapshot()
	if !mergeable(rec, s.hasChanges()) {
		return errNotMergeable
	}
	t := openTaskTree(ctx, rec.Worktree)
	switch {
	case t == nil:
		return errNotMergeable
	case a.othersIn(rec.ID, t.own.Real):
		return errMergeBusy
	}
	status, err := gitrepo.Run(ctx, t.primary.Path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(status))) > 0 {
		return errMainDirty
	}
	return a.prompt(projectID, promptArgs{Session: rec.ID, Text: mergeNowText})
}

func (a *app) cleanupChild(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	rec := s.snapshot()
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
	return !a.othersIn(rec.ID, l.own.Real) && !slices.ContainsFunc(a.children(rec.ID), func(c *session) bool { return !c.snapshot().Closed })
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
		if other != id && !rec.Closed && gitrepo.Within(gitrepo.Canonical(rec.Worktree), root) {
			return true
		}
	}
	return false
}

func (a *app) closeIdle(ctx context.Context, s *session) bool {
	s.mu.Lock()
	if s.running || len(s.queue) > 0 || s.rec.Closed || s.rec.Status != statusDone || s.asksLocked() {
		s.mu.Unlock()
		return false
	}
	s.recordLocked(actorClubmate, kindClose, acp.SessionRequest{SessionID: s.rec.AgentSessionID})
	s.rec.Closed = true
	s.setStatusLocked(statusClosed, "")
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
	if err := a.awaitOp(ctx, rec.Workspace, op.ID); err != nil {
		return fmt.Errorf("remove worktree %s: %w", l.own.Path, err)
	}
	if _, err := gitrepo.Mutate(ctx, l.parent.Path, "branch", "-d", l.own.Branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", l.own.Branch, err)
	}
	return nil
}

func (a *app) folderOf(ctx context.Context, projectID, path string) (string, error) {
	p, err := a.host.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, repo := range p.Repos {
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

func (a *app) awaitOp(ctx context.Context, projectID, id string) error {
	for {
		var st worktreeOp
		if err := a.host.RunCommand(ctx, worktreeOpCmd, projectID, map[string]string{"id": id}, &st); err != nil {
			return err
		}
		switch {
		case st.Error != "":
			return errors.New(st.Error)
		case st.Done:
			return nil
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
