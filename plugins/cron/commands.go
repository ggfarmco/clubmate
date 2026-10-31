package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const emitCommand = "triggers.emit"

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

type agentSession = agentcall.Session

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
		return a.registry.Show(cmd)
	}
	defer a.registry.Flush(ctx)
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
	return nil, fmt.Errorf("%w: %q", agentcall.ErrUnknownCommand, cmd.ID)
}

func (a *app) add(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args jobArgs
	if err := automation.Decode(cmd, &args); err != nil {
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
	caller := automation.CallerOf(cmd)
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
	if args.Owner != automation.OwnerNone {
		j.Owner = caller
	}
	j.Missed = cmp.Or(args.Missed, defaultMissed(j))
	a.mu.Lock()
	defer a.mu.Unlock()
	for j.ID == "" || a.jobs[j.ID] != nil {
		if j.ID, err = automation.NewID(idPrefix); err != nil {
			return nil, err
		}
	}
	if err := a.store.Save(ctx, j); err != nil {
		return nil, err
	}
	a.jobs[j.ID] = j
	a.registry.Touch(j.Workspace)
	a.wake()
	return j.Clone(), nil
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
	automation.Sort(jobs)
	return listResult{Jobs: jobs}
}

func (a *app) update(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args jobArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.registry.Mutate(ctx, cmd, args.ID, func(j *Job) error {
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
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.registry.Mutate(ctx, cmd, args.ID, func(j *Job) error {
		j.Enabled = false
		return nil
	})
}

func (a *app) resume(ctx context.Context, cmd pluginsdk.CommandRun) (*Job, error) {
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.registry.Mutate(ctx, cmd, args.ID, func(j *Job) error {
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
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return Run{}, err
	}
	j, err := a.registry.Authorized(ctx, cmd, args.ID)
	if err != nil {
		return Run{}, err
	}
	j = j.Clone()
	ctx = context.WithoutCancel(ctx)
	if !a.claim(j.ID) {
		r := Run{At: a.now(), Status: automation.StatusSkipped, Reason: automation.ReasonInFlight}
		return r, a.note(ctx, j.ID, r)
	}
	r := a.fire(ctx, j)
	return r, a.settle(ctx, j.ID, r, false)
}

func (a *app) remove(ctx context.Context, cmd pluginsdk.CommandRun) error {
	var args automation.IDArgs
	if err := automation.Decode(cmd, &args); err != nil {
		return err
	}
	if _, err := a.registry.Authorized(ctx, cmd, args.ID); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	j, err := a.registry.Lookup(cmd, args.ID)
	if err != nil {
		return err
	}
	if err := a.store.Remove(ctx, j); err != nil {
		return err
	}
	delete(a.jobs, j.ID)
	a.registry.Touch(j.Workspace)
	a.wake()
	return nil
}

func (a *app) validateTarget(ctx context.Context, command, args string) (json.RawMessage, error) {
	if strings.HasPrefix(command, "cron.") || strings.HasPrefix(command, "triggers.") && command != emitCommand {
		return nil, fmt.Errorf("command %q cannot be scheduled", command)
	}
	return agentcall.ValidateTarget(ctx, a.host, command, args)
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

func defaultMissed(j *Job) string {
	if j.Owner != "" {
		return missedSkip
	}
	return missedOnce
}
