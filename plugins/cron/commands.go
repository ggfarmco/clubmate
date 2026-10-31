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
	"unicode/utf8"

	"clubmate/internal/schema"
	"clubmate/pkg/pluginsdk"
)

const (
	ownerNone  = "none"
	corePlugin = "core"
)

var errUnknownCommand = errors.New("unknown command")

type idArgs struct {
	ID string `json:"id"`
}

type jobArgs struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	Args     string `json:"args"`
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
	Every    string `json:"every"`
	At       string `json:"at"`
	In       string `json:"in"`
	Owner    string `json:"owner"`
	Missed   string `json:"missed"`
}

type listResult struct {
	Jobs []*Job `json:"jobs"`
}

type target struct {
	ID     string         `json:"id"`
	Plugin string         `json:"plugin"`
	Agent  bool           `json:"agent"`
	Args   *schema.Object `json:"args"`
}

type agentSession struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	ParentSession string `json:"parentSession"`
	Status        string `json:"status"`
	Closed        bool   `json:"closed"`
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch cmd.ID {
	case "cron.list":
		return a.list(cmd), nil
	case "cron.show":
		return a.show(cmd)
	}
	defer a.flush(ctx)
	switch cmd.ID {
	case "cron.add":
		return a.add(ctx, cmd)
	case "cron.update":
		return a.update(ctx, cmd)
	case "cron.pause":
		return a.pause(ctx, cmd)
	case "cron.resume":
		return a.resume(ctx, cmd)
	case "cron.run":
		return a.runNow(ctx, cmd)
	case "cron.delete":
		return nil, a.remove(ctx, cmd)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) add(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args jobArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	s, next, err := schedule(args, a.now())
	if err != nil {
		return nil, err
	}
	targetArgs, err := a.validateTarget(ctx, args.Command, cmp.Or(args.Args, "{}"))
	if err != nil {
		return nil, err
	}
	caller := callerOf(cmd)
	j := &Job{
		Name:      args.Name,
		Workspace: cmd.ProjectID,
		Schedule:  s,
		Command:   args.Command,
		Args:      targetArgs,
		Enabled:   true,
		CreatedBy: caller,
		NextRun:   next,
	}
	if args.Owner != ownerNone {
		j.Owner = caller
	}
	j.Missed = cmp.Or(args.Missed, defaultMissed(j))
	a.mu.Lock()
	defer a.mu.Unlock()
	for j.ID == "" || a.jobs[j.ID] != nil {
		if j.ID, err = newJobID(); err != nil {
			return nil, err
		}
	}
	if err := a.store.save(ctx, j); err != nil {
		return nil, err
	}
	a.jobs[j.ID] = j
	a.touch(j.Workspace)
	a.wake()
	return clone(j), nil
}

func (a *app) list(cmd pluginsdk.CommandRun) listResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	jobs := make([]*Job, 0, len(a.jobs))
	for _, j := range a.jobs {
		if j.Workspace == cmd.ProjectID {
			c := *j
			c.History = nil
			jobs = append(jobs, &c)
		}
	}
	slices.SortFunc(jobs, func(x, y *Job) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID))
	})
	return listResult{Jobs: jobs}
}

func (a *app) show(cmd pluginsdk.CommandRun) (*Job, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	j, err := a.lookup(cmd, args.ID)
	if err != nil {
		return nil, err
	}
	return clone(j), nil
}

func (a *app) update(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args jobArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(j *Job) error {
		if args.Name != "" {
			j.Name = args.Name
		}
		if args.Missed != "" {
			j.Missed = args.Missed
		}
		if args.Command != "" || args.Args != "" {
			command := cmp.Or(args.Command, j.Command)
			raw, err := a.validateTarget(ctx, command, cmp.Or(args.Args, string(j.Args), "{}"))
			if err != nil {
				return err
			}
			j.Command, j.Args = command, raw
		}
		if args.Cron != "" || args.Timezone != "" || args.Every != "" || args.At != "" || args.In != "" {
			s, next, err := schedule(args, a.now())
			if err != nil {
				return err
			}
			j.Schedule, j.NextRun = s, next
		}
		return nil
	})
}

func (a *app) pause(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(j *Job) error {
		j.Enabled = false
		return nil
	})
}

func (a *app) resume(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(j *Job) error {
		next, ok := j.Schedule.Next(a.now())
		if !ok {
			return fmt.Errorf("job %s: schedule %s has no run after now", j.ID, j.Schedule.Describe())
		}
		j.Enabled = true
		j.Failures = 0
		j.LastError = ""
		j.NextRun = next
		return nil
	})
}

func (a *app) runNow(ctx context.Context, cmd pluginsdk.CommandRun) (Run, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return Run{}, err
	}
	j, err := a.snapshot(ctx, cmd, args.ID)
	if err != nil {
		return Run{}, err
	}
	ctx = context.WithoutCancel(ctx)
	if !a.claim(j.ID) {
		r := Run{At: a.now(), Status: statusSkipped, Reason: reasonInFlight}
		return r, a.note(ctx, j.ID, r)
	}
	r := a.fire(ctx, j)
	return r, a.settle(ctx, j.ID, r, false)
}

func (a *app) remove(ctx context.Context, cmd pluginsdk.CommandRun) error {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return err
	}
	if _, err := a.authorized(ctx, cmd, args.ID); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	j, err := a.lookup(cmd, args.ID)
	if err != nil {
		return err
	}
	if err := a.store.remove(ctx, j); err != nil {
		return err
	}
	delete(a.jobs, j.ID)
	a.touch(j.Workspace)
	a.wake()
	return nil
}

func (a *app) mutate(ctx context.Context, cmd pluginsdk.CommandRun, id string, change func(*Job) error) (*Job, error) {
	for {
		j, err := a.authorized(ctx, cmd, id)
		if err != nil {
			return nil, err
		}
		updated := clone(j)
		if err := change(updated); err != nil {
			return nil, err
		}
		saved, err := a.replace(ctx, j, updated)
		if err != nil {
			return nil, err
		}
		if saved {
			return clone(updated), nil
		}
	}
}

func (a *app) replace(ctx context.Context, current, updated *Job) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobs[updated.ID] != current {
		return false, nil
	}
	if err := a.store.save(ctx, updated); err != nil {
		return false, err
	}
	a.jobs[updated.ID] = updated
	a.touch(updated.Workspace)
	a.wake()
	return true, nil
}

func (a *app) snapshot(ctx context.Context, cmd pluginsdk.CommandRun, id string) (*Job, error) {
	j, err := a.authorized(ctx, cmd, id)
	if err != nil {
		return nil, err
	}
	return clone(j), nil
}

func (a *app) authorized(ctx context.Context, cmd pluginsdk.CommandRun, id string) (*Job, error) {
	a.mu.Lock()
	j, err := a.lookup(cmd, id)
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if allowed(cmd, j, nil) || allowed(cmd, j, a.parents(ctx, j.Workspace)) {
		return j, nil
	}
	return nil, fmt.Errorf("job %s belongs to another agent session", j.ID)
}

func (a *app) lookup(cmd pluginsdk.CommandRun, id string) (*Job, error) {
	j, ok := a.jobs[id]
	if !ok || j.Workspace != cmd.ProjectID {
		return nil, fmt.Errorf("unknown job %q", id)
	}
	return j, nil
}

func (a *app) parents(ctx context.Context, workspace string) map[string]string {
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		return nil
	}
	parents := make(map[string]string, len(sessions))
	for _, s := range sessions {
		if s.ParentSession != "" {
			parents[s.ID] = s.ParentSession
		}
	}
	return parents
}

func (a *app) validateTarget(ctx context.Context, command, args string) (json.RawMessage, error) {
	if strings.HasPrefix(command, "cron.") {
		return nil, fmt.Errorf("command %q cannot be scheduled", command)
	}
	var cmds []target
	if err := a.host.Call(ctx, "command.list", struct{}{}, &cmds); err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	i := slices.IndexFunc(cmds, func(c target) bool { return c.ID == command })
	if i < 0 {
		return nil, fmt.Errorf("%w %q", errUnknownCommand, command)
	}
	if !cmds[i].Agent {
		return nil, fmt.Errorf("command %q is not agent-visible", command)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return nil, fmt.Errorf("args: invalid JSON object: %w", err)
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(cmds[i].Args, raw); err != nil {
		return nil, err
	}
	if cmds[i].Plugin == corePlugin || cmds[i].Args != nil {
		if path := schema.Undeclared(cmds[i].Args, fields); path != "" {
			return nil, fmt.Errorf("%s: not allowed in agent calls", path)
		}
	}
	return raw, nil
}

func allowed(cmd pluginsdk.CommandRun, j *Job, parents map[string]string) bool {
	if !cmd.Agent {
		return true
	}
	if cmd.Caller == "" {
		return false
	}
	return descends(j.Owner, cmd.Caller, parents) || descends(j.CreatedBy, cmd.Caller, parents)
}

func descends(session, ancestor string, parents map[string]string) bool {
	for range len(parents) + 1 {
		if session == "" {
			return false
		}
		if session == ancestor {
			return true
		}
		session = parents[session]
	}
	return false
}

func schedule(args jobArgs, now time.Time) (Schedule, time.Time, error) {
	s, err := parseSchedule(args.Cron, args.Timezone, args.Every, args.At, args.In, now)
	if err != nil {
		return Schedule{}, time.Time{}, err
	}
	next, ok := s.Next(now)
	if !ok {
		return Schedule{}, time.Time{}, fmt.Errorf("schedule %s has no run after now", s.Describe())
	}
	return s, next, nil
}

func callerOf(cmd pluginsdk.CommandRun) string {
	if cmd.Agent {
		return cmd.Caller
	}
	return ""
}

func defaultMissed(j *Job) string {
	if j.Owner != "" {
		return missedSkip
	}
	return missedOnce
}

func clone(j *Job) *Job {
	c := *j
	c.History = slices.Clone(j.History)
	return &c
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func decode(cmd pluginsdk.CommandRun, v any) error {
	if len(cmd.Args) == 0 {
		return nil
	}
	if err := json.Unmarshal(cmd.Args, v); err != nil {
		return fmt.Errorf("%s args: %w", cmd.ID, err)
	}
	return nil
}
