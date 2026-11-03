package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	errNoWorkspace    = errors.New("command needs a workspace")
	errNeedOwner      = errors.New("pass owner: a name unique to you, e.g. your session id")
	errStartViaClaim  = errors.New("status in_progress is set by backlog.claim")
	errNotHolder      = errors.New("only the owner, an ancestor of the owner or the user can do this")
	errNotClaimed     = errors.New("task is not in progress")
	errForeignStatus  = errors.New("task is claimed by someone else; you may change only its title, description, priority, tags and note")
	errAddStatus      = errors.New("status must be idea, todo or not_now")
	errReservedOwner  = errors.New("owner user, agent and agt_… are reserved; pass a name unique to you")
	errNoSession      = errors.New("agents.new returned no session")
	errStartOption    = errors.New("invalid_params")
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

type linkView struct {
	Link
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
}

type taskView struct {
	Task
	Links           []linkView       `json:"links,omitempty"`
	Attachments     []attachmentView `json:"attachments,omitempty"`
	Primary         *primaryView     `json:"primary,omitempty"`
	DescriptionHTML string           `json:"descriptionHtml,omitempty"`
}

type startResult struct {
	Session string `json:"session"`
}

type commandArgs struct {
	ID          int           `json:"id"`
	Status      *string       `json:"status"`
	Limit       int           `json:"limit"`
	Title       *string       `json:"title"`
	Description *string       `json:"description"`
	Priority    *string       `json:"priority"`
	Tags        *[]string     `json:"tags"`
	Links       *[]Link       `json:"links"`
	Attachments *[]Attachment `json:"attachments"`
	Tag         string        `json:"tag"`
	Note        *string       `json:"note"`
	Owner       string        `json:"owner"`
	keepClaim   bool
	startOptions
}

type startOptions struct {
	Agent  string `json:"agent"`
	Mode   string `json:"mode"`
	Where  string `json:"where"`
	Prompt string `json:"prompt"`
}

func (o startOptions) check() error {
	switch {
	case o.Agent != "" && o.Agent != agentClaude:
		return fmt.Errorf("%w: unknown agent %q", errStartOption, o.Agent)
	case o.Mode != "" && !slices.ContainsFunc(api.ClaudeModes, func(m api.Mode) bool { return m.ID == o.Mode }):
		return fmt.Errorf("%w: unknown mode %q", errStartOption, o.Mode)
	case o.Where != "" && o.Where != whereNew:
		return fmt.Errorf("%w: where must be empty or %q", errStartOption, whereNew)
	}
	return nil
}

func (b *backlog) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.WorkspaceID == "" {
		return nil, errNoWorkspace
	}
	var args commandArgs
	if len(cmd.Args) > 0 && string(cmd.Args) != "null" {
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, err
		}
	}
	switch cmd.ID {
	case "backlog.list":
		return b.listCmd(ctx, cmd.WorkspaceID, args)
	case "backlog.get":
		return b.getCmd(ctx, cmd, args)
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
	case "backlog.start":
		session, err := b.startAgent(ctx, cmd.WorkspaceID, args.ID, args.startOptions)
		if err != nil {
			return nil, err
		}
		return startResult{Session: session}, nil
	case "backlog.takeback":
		if err := b.takeBack(ctx, cmd.WorkspaceID, args.ID); err != nil {
			return nil, err
		}
		return b.store.get(ctx, cmd.WorkspaceID, args.ID)
	case "backlog.new":
		return nil, b.h.FocusField(ctx, cmd.WorkspaceID, sectionID)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (b *backlog) getCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	t, err := b.store.get(ctx, cmd.WorkspaceID, args.ID)
	if err != nil {
		return nil, err
	}
	linked, err := b.linked(ctx, cmd.WorkspaceID, t)
	if err != nil {
		return nil, err
	}
	t.Owners = ownerHistory(t)
	out := taskView{Task: t, Links: make([]linkView, 0, len(t.Links)), Attachments: make([]attachmentView, 0, len(t.Attachments))}
	for _, l := range t.Links {
		out.Links = append(out.Links, linkView{Link: l, Title: linked[l.Task].Title, Status: linked[l.Task].Status})
	}
	root := b.root(ctx, cmd.WorkspaceID)
	for _, a := range t.Attachments {
		out.Attachments = append(out.Attachments, attachmentOf(a, root))
	}
	if _, agent := creatorOf(cmd); agent {
		return out, nil
	}
	out.Primary = primary(&t, b.ownerLabels(ctx, cmd.WorkspaceID, &t)[t.ClaimedBy])
	all, err := b.store.list(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]Task, len(all))
	for _, o := range all {
		byID[o.ID] = o
	}
	out.DescriptionHTML = describe(t.Description, byID)
	return out, nil
}

func (b *backlog) linked(ctx context.Context, workspaceID string, t Task) (map[int]Task, error) {
	out := make(map[int]Task, len(t.Links))
	for _, l := range t.Links {
		other, err := b.store.get(ctx, workspaceID, l.Task)
		if errors.Is(err, errTaskNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[l.Task] = other
	}
	return out, nil
}

func (b *backlog) listCmd(ctx context.Context, workspaceID string, args commandArgs) (any, error) {
	filter := statusOpen
	if args.Status != nil && *args.Status != "" {
		filter = *args.Status
	}
	if filter != statusOpen && !validStatus(filter) {
		return nil, errBadStatus
	}
	all, err := b.store.list(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(args.Tag), "#"))
	tasks := make([]Task, 0, len(all))
	for _, t := range all {
		if tag != "" && !slices.Contains(t.Tags, tag) {
			continue
		}
		if filter == t.Status || filter == statusOpen && (t.Status == statusIdea || t.Status == statusTodo || t.Status == statusInProgress) {
			t.Owners = nil
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
	priority := priorityNormal
	if args.Priority != nil && *args.Priority != "" {
		priority = *args.Priority
	}
	if !validPriority(priority) {
		return nil, errBadPriority
	}
	t := Task{Title: title, Description: deref(args.Description), Status: status, Priority: priority, CreatedBy: createdBy}
	if err := applyLists(&t, args); err != nil {
		return nil, err
	}
	t, err = b.store.add(ctx, cmd.WorkspaceID, t)
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.WorkspaceID, t.ID, kindAdded)
	return t, nil
}

func (b *backlog) updateCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	self, isAgent := creatorOf(cmd)
	t, err := b.store.mutate(ctx, cmd.WorkspaceID, args.ID, func(t *Task) error {
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
		if err := applyLists(t, args); err != nil {
			return err
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
		case isAgent && t.Status == statusInProgress && t.ClaimedBy != self:
			return errForeignStatus
		}
		if t.Status == statusInProgress {
			releaseClaim(t, whyReleased)
		}
		t.Status = status
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.WorkspaceID, t.ID, kindUpdated)
	return t, nil
}

func (b *backlog) claimCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	return b.claim(ctx, cmd.WorkspaceID, args.ID, b.ownerMove(ctx, cmd.WorkspaceID, who.Owner, whyClaimed), nil)
}

func (b *backlog) claim(ctx context.Context, workspaceID string, id int, mv OwnerMove, check func(*Task) error) (Task, error) {
	t, err := b.store.mutate(ctx, workspaceID, id, func(t *Task) error {
		if check != nil {
			if err := check(t); err != nil {
				return err
			}
		}
		switch t.Status {
		case statusIdea, statusTodo:
			t.ClaimedFrom = t.Status
		case statusInProgress:
			if t.ClaimedBy == mv.Owner {
				return nil
			}
			if !(mv.Why == whyStarted && t.ClaimedBy == userOwner) && !b.live.stale(ctx, workspaceID, t.ClaimedBy) {
				return fmt.Errorf("task #%d is in progress by %s; pick another task", t.ID, t.ClaimedBy)
			}
			t.LastClaimedBy = t.ClaimedBy
		default:
			return fmt.Errorf("task #%d is %s and cannot be claimed; pick another task", t.ID, t.Status)
		}
		t.Status = statusInProgress
		t.ClaimedBy = mv.Owner
		t.ClaimedAt = time.Now().UTC()
		t.move(mv)
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	b.changed(ctx, workspaceID, t.ID, kindClaimed)
	return t, nil
}

func (b *backlog) ownerMove(ctx context.Context, workspaceID, owner, why string) OwnerMove {
	mv := OwnerMove{Owner: owner, Why: why}
	if !strings.HasPrefix(owner, sessionPfx) {
		return mv
	}
	if s, ok := b.live.session(ctx, workspaceID, owner); ok {
		mv.Title = s.Title
		if s.Run != nil {
			mv.Run, mv.RunTitle = s.Run.ID, s.Run.Title
		}
	}
	return mv
}

func (b *backlog) takeBack(ctx context.Context, workspaceID string, id int) error {
	t, err := b.store.mutate(ctx, workspaceID, id, func(t *Task) error {
		if t.Status != statusInProgress || t.ClaimedBy == userOwner {
			return fmt.Errorf("task #%d is not owned by someone else", t.ID)
		}
		t.LastClaimedBy = t.ClaimedBy
		t.ClaimedBy = userOwner
		t.ClaimedAt = time.Now().UTC()
		t.move(OwnerMove{Owner: userOwner, Why: whyTakenBack})
		return nil
	})
	if err != nil {
		return err
	}
	b.changed(ctx, workspaceID, t.ID, kindClaimed)
	return nil
}

func (b *backlog) startAgent(ctx context.Context, workspaceID string, id int, opts startOptions) (string, error) {
	if err := opts.check(); err != nil {
		return "", err
	}
	t, err := b.store.get(ctx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if err := startable(ctx, b.live, workspaceID, &t); err != nil {
		return "", err
	}
	linked, err := b.linked(ctx, workspaceID, t)
	if err != nil {
		return "", err
	}
	body := startBody(t, linked)
	if p := strings.TrimSpace(opts.Prompt); p != "" {
		body = p
	}
	args := map[string]string{"prompt": body + "\n\n" + ownsLine(t.ID), "title": t.Title}
	if opts.Mode != "" {
		args["firstMode"] = opts.Mode
	}
	if opts.Where == whereNew {
		args["branch"] = branchFor(&t)
	}
	var res startResult
	if err := b.h.RunCommand(ctx, "agents.new", workspaceID, args, &res); err != nil {
		return "", err
	}
	if res.Session == "" {
		return "", errNoSession
	}
	b.live.invalidate(workspaceID)
	_, err = b.claim(ctx, workspaceID, id, OwnerMove{Owner: res.Session, Title: t.Title, Why: whyStarted}, func(t *Task) error {
		return startable(ctx, b.live, workspaceID, t)
	})
	return res.Session, err
}

func startable(ctx context.Context, live liveness, workspaceID string, t *Task) error {
	switch {
	case t.Status == statusIdea || t.Status == statusTodo:
		return nil
	case t.Status == statusInProgress && (t.ClaimedBy == userOwner || live.stale(ctx, workspaceID, t.ClaimedBy)):
		return nil
	}
	return fmt.Errorf("task #%d is %s and cannot get an agent", t.ID, t.Status)
}

func (b *backlog) sessionStopped(ctx context.Context, workspaceID, session string) {
	tasks, err := b.store.list(ctx, workspaceID)
	if err != nil {
		logError("stopped "+session, err)
		return
	}
	for _, t := range tasks {
		if t.Status != statusInProgress || t.ClaimedBy != session {
			continue
		}
		_, err := b.store.mutate(ctx, workspaceID, t.ID, func(t *Task) error {
			if t.Status != statusInProgress || t.ClaimedBy != session {
				return nil
			}
			status := cmp.Or(t.ClaimedFrom, statusIdea)
			releaseClaim(t, whyStopped)
			t.Status = status
			return nil
		})
		logError("stopped "+session, err)
		b.changed(ctx, workspaceID, t.ID, kindReleased)
	}
}

func (b *backlog) mayFinish(ctx context.Context, workspaceID string, who actor, t *Task) bool {
	return !who.Agent || who.Owner == t.ClaimedBy || who.Session != "" && b.live.ancestor(ctx, workspaceID, who.Session, t.ClaimedBy)
}

func (b *backlog) releaseCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	t, err := b.store.mutate(ctx, cmd.WorkspaceID, args.ID, func(t *Task) error {
		if t.Status != statusInProgress {
			return fmt.Errorf("task #%d is %s: %w", t.ID, t.Status, errNotClaimed)
		}
		if !b.mayFinish(ctx, cmd.WorkspaceID, who, t) {
			return fmt.Errorf("task #%d is in progress by %s: %w", t.ID, t.ClaimedBy, errNotHolder)
		}
		status := cmp.Or(t.ClaimedFrom, statusIdea)
		releaseClaim(t, whyReleased)
		t.Status = status
		if args.Note != nil {
			t.Note = *args.Note
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	b.changed(ctx, cmd.WorkspaceID, t.ID, kindReleased)
	return t, nil
}

func (b *backlog) doneCmd(ctx context.Context, cmd pluginsdk.CommandRun, args commandArgs) (any, error) {
	who, err := actorOf(cmd, args.Owner)
	if err != nil {
		return nil, err
	}
	t, err := b.store.mutate(ctx, cmd.WorkspaceID, args.ID, func(t *Task) error {
		if t.Status == statusInProgress && !b.mayFinish(ctx, cmd.WorkspaceID, who, t) && !b.live.stale(ctx, cmd.WorkspaceID, t.ClaimedBy) {
			return fmt.Errorf("task #%d is in progress by %s: %w", t.ID, t.ClaimedBy, errNotHolder)
		}
		if t.Status == statusInProgress {
			releaseClaim(t, whyDone)
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
	b.changed(ctx, cmd.WorkspaceID, t.ID, kindDone)
	return t, nil
}

func releaseClaim(t *Task, why string) {
	t.LastClaimedBy = t.ClaimedBy
	t.ClaimedBy = ""
	t.ClaimedAt = time.Time{}
	t.ClaimedFrom = ""
	t.move(OwnerMove{Owner: userOwner, Why: why})
}

func applyLists(t *Task, args commandArgs) error {
	if args.Tags != nil {
		tags, err := cleanTags(*args.Tags)
		if err != nil {
			return err
		}
		t.Tags = tags
	}
	if args.Links != nil {
		links, err := cleanLinks(t.ID, *args.Links)
		if err != nil {
			return err
		}
		t.Links = links
	}
	if args.Attachments != nil {
		attachments, err := cleanAttachments(*args.Attachments)
		if err != nil {
			return err
		}
		t.Attachments = attachments
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func startBody(t Task, linked map[int]Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Backlog task #%d: %s\n", t.ID, t.Title)
	if desc := strings.TrimSpace(t.Description); desc != "" {
		b.WriteString("\n" + desc + "\n")
	}
	if len(t.Links) > 0 {
		b.WriteString("\nLinks:\n")
		for _, l := range t.Links {
			fmt.Fprintf(&b, "- %s #%d", linkWords[l.Kind], l.Task)
			if o, ok := linked[l.Task]; ok {
				fmt.Fprintf(&b, " %s (%s)", o.Title, strings.ToLower(statusLabels[o.Status]))
			}
			b.WriteString("\n")
		}
	}
	if len(t.Attachments) > 0 {
		b.WriteString("\nAttachments (paths are relative to the worktree):\n")
		for _, a := range t.Attachments {
			fmt.Fprintf(&b, "- %s %s", a.Kind, a.Ref)
			if a.Title != "" {
				fmt.Fprintf(&b, " (%s)", a.Title)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func ownsLine(id int) string {
	return fmt.Sprintf("You own backlog task #%d. Finish it with backlog_done and a note naming the commit, or backlog_release with a note if you stop.", id)
}
