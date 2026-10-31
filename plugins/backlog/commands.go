package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	userOwner  = "user"
	agentOwner = "agent"
	sessionPfx = api.IDPrefix + "_"

	kindAdded    = "added"
	kindUpdated  = "updated"
	kindClaimed  = "claimed"
	kindReleased = "released"
	kindDone     = "done"

	statusOpen = "open"
)

var (
	errUnknownCommand = errors.New("unknown command")
	errNoProject      = errors.New("command needs a project")
	errNeedOwner      = errors.New("pass owner: a name unique to you, e.g. your session id")
	errAgentTodo      = errors.New("only the user puts tasks into todo; add it as idea")
	errStartViaClaim  = errors.New("status in_progress is set by backlog.claim")
	errNotHolder      = errors.New("only the owner, an ancestor of the owner or the user can do this")
	errNotClaimed     = errors.New("task is not in progress")
	errForeignStatus  = errors.New("task is claimed by someone else; you may change only its title, description, priority and note")
	errAddStatus      = errors.New("status must be idea, todo or not_now")
	errReservedOwner  = errors.New("owner user, agent and agt_… are reserved; pass a name unique to you")
)

type actor struct {
	Owner   string
	Agent   bool
	Session string
}

func actorOf(cmd pluginsdk.CommandRun, ownerArg string) (actor, error) {
	switch {
	case cmd.Caller != "":
		a := actor{Owner: cmd.Caller, Agent: true}
		if strings.HasPrefix(cmd.Caller, sessionPfx) {
			a.Session = cmd.Caller
		}
		return a, nil
	case cmd.Agent:
		if ownerArg == "" {
			return actor{}, errNeedOwner
		}
		if ownerArg == userOwner || ownerArg == agentOwner || strings.HasPrefix(ownerArg, sessionPfx) {
			return actor{}, errReservedOwner
		}
		return actor{Owner: ownerArg, Agent: true}, nil
	}
	return actor{Owner: userOwner}, nil
}

func creatorOf(cmd pluginsdk.CommandRun) (string, bool) {
	switch {
	case cmd.Caller != "":
		return cmd.Caller, true
	case cmd.Agent:
		return agentOwner, true
	}
	return userOwner, false
}

type commandArgs struct {
	ID          int     `json:"id"`
	Status      *string `json:"status"`
	Limit       int     `json:"limit"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *string `json:"priority"`
	Note        *string `json:"note"`
	Owner       string  `json:"owner"`
	keepClaim   bool
}

func (b *backlog) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ProjectID == "" {
		return nil, errNoProject
	}
	var args commandArgs
	if len(cmd.Args) > 0 && string(cmd.Args) != "null" {
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, err
		}
	}
	switch cmd.ID {
	case "backlog.list":
		return b.listCmd(ctx, cmd.ProjectID, args)
	case "backlog.get":
		return b.store.get(ctx, cmd.ProjectID, args.ID)
	case "backlog.add":
		return b.addCmd(ctx, cmd, args)
	case "backlog.update":
		return b.updateCmd(ctx, cmd, args)
	case "backlog.claim":
		return b.claimCmd(ctx, cmd, args)
	case "backlog.release":
		return b.releaseCmd(ctx, cmd, args)
	case "backlog.done":
		return b.doneCmd(ctx, cmd, args)
	case "backlog.new":
		return nil, b.h.FocusField(ctx, cmd.ProjectID, sectionID)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (b *backlog) listCmd(ctx context.Context, projectID string, args commandArgs) (any, error) {
	filter := statusOpen
	if args.Status != nil && *args.Status != "" {
		filter = *args.Status
	}
	if filter != statusOpen && !validStatus(filter) {
		return nil, errBadStatus
	}
	all, err := b.store.list(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(all))
	for _, t := range all {
		if filter == t.Status || filter == statusOpen && (t.Status == statusIdea || t.Status == statusTodo || t.Status == statusInProgress) {
			tasks = append(tasks, t)
		}
	}
	if args.Limit > 0 && len(tasks) > args.Limit {
		tasks = tasks[:args.Limit]
	}
	return map[string]any{"tasks": tasks}, nil
}

func (b *backlog) addCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	createdBy, isAgent := creatorOf(cmd)
	title, err := cleanTitle(deref(args.Title))
	if err != nil {
		return nil, err
	}
	if err := checkDescription(deref(args.Description)); err != nil {
		return nil, err
	}
	status := statusTodo
	if isAgent {
		status = statusIdea
	}
	if args.Status != nil && *args.Status != "" {
		status = *args.Status
	}
	if status != statusIdea && status != statusTodo && status != statusNotNow {
		return nil, errAddStatus
	}
	if isAgent && status == statusTodo {
		return nil, errAgentTodo
	}
	priority := priorityNormal
	if args.Priority != nil && *args.Priority != "" {
		priority = *args.Priority
	}
	if !validPriority(priority) {
		return nil, errBadPriority
	}
	t, err := b.store.add(ctx, cmd.ProjectID, Task{Title: title, Description: deref(args.Description), Status: status, Priority: priority, CreatedBy: createdBy})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.ProjectID, t.ID, kindAdded)
	return t, nil
}

func (b *backlog) updateCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	self, isAgent := creatorOf(cmd)
	t, err := b.store.mutate(ctx, cmd.ProjectID, args.ID, func(t *Task) error {
		if args.Title != nil {
			title, err := cleanTitle(*args.Title)
			if err != nil {
				return err
			}
			t.Title = title
		}
		if args.Description != nil {
			if err := checkDescription(*args.Description); err != nil {
				return err
			}
			t.Description = *args.Description
		}
		if args.Priority != nil {
			if !validPriority(*args.Priority) {
				return errBadPriority
			}
			t.Priority = *args.Priority
		}
		if args.Note != nil {
			t.Note = *args.Note
		}
		if args.Status == nil || *args.Status == t.Status || args.keepClaim && t.Status == statusInProgress {
			return nil
		}
		status := *args.Status
		switch {
		case !validStatus(status):
			return errBadStatus
		case status == statusInProgress:
			return errStartViaClaim
		case isAgent && status == statusTodo:
			return errAgentTodo
		case isAgent && t.Status == statusInProgress && t.ClaimedBy != self:
			return errForeignStatus
		}
		if t.Status == statusInProgress {
			releaseClaim(t)
		}
		t.Status = status
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.ProjectID, t.ID, kindUpdated)
	return t, nil
}

func (b *backlog) claimCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	t, err := b.store.mutate(ctx, cmd.ProjectID, args.ID, func(t *Task) error {
		switch t.Status {
		case statusIdea, statusTodo:
			t.ClaimedFrom = t.Status
		case statusInProgress:
			if t.ClaimedBy == who.Owner {
				return nil
			}
			if !b.live.stale(ctx, cmd.ProjectID, t.ClaimedBy) {
				return fmt.Errorf("task #%d is in progress by %s; pick another task", t.ID, t.ClaimedBy)
			}
			t.LastClaimedBy = t.ClaimedBy
		default:
			return fmt.Errorf("task #%d is %s and cannot be claimed; pick another task", t.ID, t.Status)
		}
		t.Status = statusInProgress
		t.ClaimedBy = who.Owner
		t.ClaimedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.ProjectID, t.ID, kindClaimed)
	return t, nil
}

func (b *backlog) mayFinish(ctx context.Context, projectID string, who actor, t *Task) bool {
	return !who.Agent || who.Owner == t.ClaimedBy || who.Session != "" && b.live.ancestor(ctx, projectID, who.Session, t.ClaimedBy)
}

func (b *backlog) releaseCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	t, err := b.store.mutate(ctx, cmd.ProjectID, args.ID, func(t *Task) error {
		if t.Status != statusInProgress {
			return fmt.Errorf("task #%d is %s: %w", t.ID, t.Status, errNotClaimed)
		}
		if !b.mayFinish(ctx, cmd.ProjectID, who, t) {
			return fmt.Errorf("task #%d is in progress by %s: %w", t.ID, t.ClaimedBy, errNotHolder)
		}
		status := t.ClaimedFrom
		if status == "" {
			status = statusIdea
		}
		releaseClaim(t)
		t.Status = status
		if args.Note != nil {
			t.Note = *args.Note
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.ProjectID, t.ID, kindReleased)
	return t, nil
}

func (b *backlog) doneCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	t, err := b.store.mutate(ctx, cmd.ProjectID, args.ID, func(t *Task) error {
		if t.Status == statusInProgress && !b.mayFinish(ctx, cmd.ProjectID, who, t) && !b.live.stale(ctx, cmd.ProjectID, t.ClaimedBy) {
			return fmt.Errorf("task #%d is in progress by %s: %w", t.ID, t.ClaimedBy, errNotHolder)
		}
		if t.Status == statusInProgress {
			releaseClaim(t)
		}
		t.Status = statusDone
		if args.Note != nil {
			t.Note = *args.Note
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.ProjectID, t.ID, kindDone)
	return t, nil
}

func releaseClaim(t *Task) {
	t.LastClaimedBy = t.ClaimedBy
	t.ClaimedBy = ""
	t.ClaimedAt = time.Time{}
	t.ClaimedFrom = ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
