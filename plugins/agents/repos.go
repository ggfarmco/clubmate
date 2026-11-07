package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"clubmate/pkg/pluginsdk"
)

const (
	repoFixFormat  = "[Clubmate] %s failed and the user asks you to fix it.\n\n"
	repoFixTail    = "Where it failed:\n\n%s\n\n"
	repoFixRule    = "Fix it, then report with agents_task_report: ready, or failed with tail."
	worktreeSwitch = "git.worktree-switch"
)

var (
	errRepoNotFailed = errors.New("this repo hasn't failed")
	errRepoNoChild   = errors.New("this repo has no open session to ask")
)

type worktreeSwitchArgs struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

func (a *app) repoFixCommand(ctx context.Context, cmd pluginsdk.CommandRun, args repoLeaveArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errLandUser
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	a.landMu.Lock()
	defer a.landMu.Unlock()
	owner := a.handedTo(s)
	task := owner.snapshot().Task.clone()
	i := task.repoIndex(args.Repo)
	if i < 0 {
		return nil, errNotRepoChild
	}
	r := &task.Repos[i]
	switch {
	case r.State != landFailed:
		return nil, errRepoNotFailed
	case r.FixAsked:
		return nil, nil
	}
	kid := a.session(r.Session)
	if kid == nil || kid.snapshot().closed() {
		return nil, errRepoNoChild
	}
	if err := a.submit(kid, &promptArgs{Text: r.fixText()}); err != nil {
		return nil, err
	}
	r.FixAsked = true
	owner.putTask(task)
	a.saveNow(ctx, owner)
	return nil, nil
}

func (r taskRepo) fixText() string {
	text := fmt.Sprintf(repoFixFormat, r.Name)
	if tail := strings.TrimSpace(r.Tail); tail != "" {
		text += fmt.Sprintf(repoFixTail, tail)
	}
	return text + repoFixRule
}

func isReposAction(action string) bool {
	switch action {
	case actReposToggle, actReposRow, actReposShow, actReposKey, actRepoFix, actRepoSession, actRepoCode, actRepoLeave:
		return true
	}
	return false
}

func (a *app) reposAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, st *sessionUI, node string) error {
	rec := s.snapshot()
	if rec.Task == nil {
		return nil
	}
	task := *rec.Task
	switch ev.Action {
	case actReposToggle:
		a.uiMu.Lock()
		st.repos.toggleCard(task)
		a.uiMu.Unlock()
		return nil
	case actReposShow:
		return a.showRepos(ctx, ev, rec, st, s.currentTurn())
	case actReposRow:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		a.toggleRepoRow(st, task, id)
		return nil
	case actReposKey:
		key, err := itemID(ev)
		if err != nil {
			return err
		}
		return a.reposKey(ctx, ev, s, st, task, key)
	}
	i := task.rowIndex(repoRowOf(node))
	if i < 0 {
		return nil
	}
	return a.repoAction(ctx, ev, ev.Action, s, task.Repos[i])
}

func repoRowOf(node string) string {
	parts := strings.Split(node, "/")
	if len(parts) <= reposRowPart {
		return ""
	}
	return parts[reposRowPart]
}

func (a *app) repoAction(ctx context.Context, ev pluginsdk.UIEvent, action string, s *session, r taskRepo) error {
	cmd := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID, Agent: ev.Agent, Caller: ev.Caller}
	args := repoLeaveArgs{Session: s.snapshot().ID, Repo: r.Path}
	switch action {
	case actRepoFix:
		_, err := a.repoFixCommand(ctx, cmd, args)
		return err
	case actRepoLeave:
		_, err := a.repoLeaveCommand(ctx, cmd, args)
		return err
	case actRepoSession:
		return a.show(ctx, ev, r.Session)
	case actRepoCode:
		return a.openRepoInCode(ctx, ev.WorkspaceID, r)
	}
	return nil
}

func (a *app) openRepoInCode(ctx context.Context, workspaceID string, r taskRepo) error {
	kid := a.session(r.Session)
	if kid == nil {
		return errRepoNoChild
	}
	args := worktreeSwitchArgs{Repo: r.Path, Path: kid.snapshot().Worktree}
	if err := a.host.RunCommand(ctx, worktreeSwitch, workspaceID, args, nil); err != nil {
		return err
	}
	return a.host.Focus(ctx, workspaceID, editorSection, codeSlot)
}

func (a *app) showRepos(ctx context.Context, ev pluginsdk.UIEvent, rec record, st *sessionUI, turn int) error {
	a.uiMu.Lock()
	st.repos.openCard(*rec.Task)
	st.anchor, st.anchorTurn = reposKeysID(rec.ID), turn
	st.anchorRev++
	a.uiMu.Unlock()
	return a.host.FocusNode(ctx, ev.WorkspaceID, sectionID, mainSlot, reposKeysID(rec.ID))
}

func (a *app) toggleRepoRow(st *sessionUI, task taskRecord, id string) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.repos.cursor = id
	if i := task.rowIndex(id); i >= 0 {
		st.repos.toggleRow(task.Repos[i])
	}
}

func (a *app) reposKey(ctx context.Context, ev pluginsdk.UIEvent, s *session, st *sessionUI, task taskRecord, key string) error {
	a.uiMu.Lock()
	cursor := st.repos.current(task)
	switch key {
	case "up", "k":
		cursor = max(cursor-1, 0)
	case "down", "j":
		cursor = min(cursor+1, len(task.Repos)-1)
	}
	st.repos.cursor = repoRowID(task.Repos[cursor].Path)
	a.uiMu.Unlock()
	if key != "return" {
		return nil
	}
	r := task.Repos[cursor]
	if r.State == landFailed && !r.FixAsked && a.repoChild(r) {
		return a.repoAction(ctx, ev, actRepoFix, s, r)
	}
	a.toggleRepoRow(st, task, repoRowID(r.Path))
	return nil
}

func (a *app) repoChild(r taskRepo) bool {
	kid := a.session(r.Session)
	return kid != nil && !kid.snapshot().closed()
}

func (a *app) repoKids(rec record) map[string]record {
	if rec.Task == nil {
		return nil
	}
	kids := map[string]record{}
	for _, r := range rec.Task.Repos {
		if kid := a.session(r.Session); kid != nil {
			kids[r.Session] = kid.snapshot()
		}
	}
	return kids
}
