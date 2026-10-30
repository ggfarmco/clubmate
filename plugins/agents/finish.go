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
)

var defaultBranches = []string{"main", "master"}

type childLayout struct {
	primary   gitrepo.Worktree
	own       gitrepo.Worktree
	parent    gitrepo.Worktree
	onDefault bool
}

type worktreeSession struct {
	ID string `json:"id"`
}

func (a *app) policies(ctx context.Context, rec record) []acp.ContentBlock {
	out := []acp.ContentBlock{{Type: "text", Text: finishPolicy(rec, a.layout(ctx, rec))}}
	if len(a.children(rec.ID)) > 0 {
		out = append(out, acp.ContentBlock{Type: "text", Text: orchestratorText})
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
	f, err := gitrepo.OpenFolder(ctx, rec.Worktree)
	if err != nil {
		return nil
	}
	primary, own := f.Trees[0], f.Checkout
	p := gitrepo.Longest(f.Trees, gitrepo.Canonical(parent.snapshot().Worktree))
	switch {
	case p == nil, own.Branch == "", p.Branch == "", own.Real == p.Real, own.Real == primary.Real:
		return nil
	case !strings.HasPrefix(own.Real, filepath.Join(primary.Real, worktreesDir)+string(filepath.Separator)):
		return nil
	}
	base, _ := gitrepo.DefaultBase(ctx, primary.Path)
	_, remoteBase, _ := strings.Cut(base, "/")
	isDefault := func(branch string) bool {
		return branch == base || branch == remoteBase || slices.Contains(defaultBranches, branch)
	}
	if isDefault(own.Branch) {
		return nil
	}
	return &childLayout{primary: primary, own: own, parent: *p, onDefault: isDefault(p.Branch)}
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
