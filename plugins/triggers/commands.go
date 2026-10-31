package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
)

const ownerNone = "none"

var errNoName = errors.New("name is required")

type idArgs struct {
	ID string `json:"id"`
}

type triggerArgs struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	On      string  `json:"on"`
	Match   *string `json:"match"`
	Command string  `json:"command"`
	Args    string  `json:"args"`
	Owner   string  `json:"owner"`
}

type emitArgs struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
}

type emitResult struct {
	Topic string `json:"topic"`
	Seq   int64  `json:"seq"`
}

type listResult struct {
	Triggers []*Trigger `json:"triggers"`
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch cmd.ID {
	case "triggers.list":
		return a.list(cmd), nil
	case "triggers.show":
		return a.show(cmd)
	case "triggers.emit":
		return a.emit(ctx, cmd)
	}
	defer a.flush(ctx)
	switch cmd.ID {
	case "triggers.add":
		return a.add(ctx, cmd)
	case "triggers.update":
		return a.update(ctx, cmd)
	case "triggers.pause":
		return a.pause(ctx, cmd)
	case "triggers.resume":
		return a.resume(ctx, cmd)
	case "triggers.delete":
		return nil, a.remove(ctx, cmd)
	}
	return nil, fmt.Errorf("%w: %q", agentcall.ErrUnknownCommand, cmd.ID)
}

func (a *app) add(ctx context.Context, cmd pluginsdk.CommandRun) (*Trigger, error) {
	var args triggerArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	if args.Name == "" {
		return nil, errNoName
	}
	if err := checkPattern(args.On); err != nil {
		return nil, err
	}
	var matchText string
	if args.Match != nil {
		matchText = *args.Match
	}
	match, err := parseMatch(matchText)
	if err != nil {
		return nil, err
	}
	targetArgs, err := a.validateTarget(ctx, args.Command, cmp.Or(args.Args, "{}"))
	if err != nil {
		return nil, err
	}
	caller := callerOf(cmd)
	t := &Trigger{
		Name:      args.Name,
		Workspace: cmd.ProjectID,
		On:        args.On,
		Match:     match,
		Command:   args.Command,
		Args:      targetArgs,
		Enabled:   true,
		CreatedBy: caller,
	}
	if args.Owner != ownerNone {
		t.Owner = caller
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for t.ID == "" || a.triggers[t.ID] != nil {
		if t.ID, err = newTriggerID(); err != nil {
			return nil, err
		}
	}
	if err := a.put(ctx, t); err != nil {
		delete(a.triggers, t.ID)
		return nil, err
	}
	return clone(t), nil
}

func (a *app) list(cmd pluginsdk.CommandRun) listResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	triggers := make([]*Trigger, 0, len(a.triggers))
	for _, t := range a.triggers {
		if t.Workspace == cmd.ProjectID {
			c := *t
			c.History = nil
			triggers = append(triggers, &c)
		}
	}
	sortTriggers(triggers)
	return listResult{Triggers: triggers}
}

func (a *app) show(cmd pluginsdk.CommandRun) (*Trigger, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t, err := a.lookup(cmd, args.ID)
	if err != nil {
		return nil, err
	}
	return clone(t), nil
}

func (a *app) update(ctx context.Context, cmd pluginsdk.CommandRun) (*Trigger, error) {
	var args triggerArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(t *Trigger) error {
		if args.Name != "" {
			t.Name = args.Name
		}
		if args.On != "" {
			if err := checkPattern(args.On); err != nil {
				return err
			}
			t.On = args.On
		}
		if args.Match != nil {
			match, err := parseMatch(*args.Match)
			if err != nil {
				return err
			}
			t.Match = match
		}
		if args.Command != "" || args.Args != "" {
			command := cmp.Or(args.Command, t.Command)
			raw, err := a.validateTarget(ctx, command, cmp.Or(args.Args, string(t.Args), "{}"))
			if err != nil {
				return err
			}
			t.Command, t.Args = command, raw
		}
		return nil
	})
}

func (a *app) pause(ctx context.Context, cmd pluginsdk.CommandRun) (*Trigger, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(t *Trigger) error {
		t.Enabled = false
		return nil
	})
}

func (a *app) resume(ctx context.Context, cmd pluginsdk.CommandRun) (*Trigger, error) {
	var args idArgs
	if err := decode(cmd, &args); err != nil {
		return nil, err
	}
	return a.mutate(ctx, cmd, args.ID, func(t *Trigger) error {
		t.Enabled = true
		t.Failures = 0
		t.LastError = ""
		return nil
	})
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
	t, err := a.lookup(cmd, args.ID)
	if err != nil {
		return err
	}
	return a.drop(ctx, t)
}

func (a *app) emit(ctx context.Context, cmd pluginsdk.CommandRun) (emitResult, error) {
	var args emitArgs
	if err := decode(cmd, &args); err != nil {
		return emitResult{}, err
	}
	if args.Name == "" {
		return emitResult{}, errNoName
	}
	payload := map[string]any{}
	if strings.TrimSpace(args.Payload) != "" {
		if err := json.Unmarshal([]byte(args.Payload), &payload); err != nil {
			return emitResult{}, fmt.Errorf("payload: invalid JSON object: %w", err)
		}
		if payload == nil {
			payload = map[string]any{}
		}
	}
	payload["workspace"] = cmd.ProjectID
	delete(payload, byField)
	if cmd.Agent {
		payload[byField] = cmp.Or(cmd.Caller, byAgent)
	}
	topic := emitPrefix + args.Name
	ev, err := a.host.Publish(ctx, topic, payload)
	if err != nil {
		return emitResult{}, fmt.Errorf("emit %s: %w", topic, err)
	}
	return emitResult{Topic: ev.Topic, Seq: ev.Seq}, nil
}

func (a *app) mutate(ctx context.Context, cmd pluginsdk.CommandRun, id string, change func(*Trigger) error) (*Trigger, error) {
	for {
		t, err := a.authorized(ctx, cmd, id)
		if err != nil {
			return nil, err
		}
		updated := clone(t)
		if err := change(updated); err != nil {
			return nil, err
		}
		saved, err := a.replace(ctx, t, updated)
		if err != nil {
			return nil, err
		}
		if saved {
			return clone(updated), nil
		}
	}
}

func (a *app) replace(ctx context.Context, current, updated *Trigger) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.triggers[updated.ID] != current {
		return false, nil
	}
	if err := a.store.save(ctx, updated); err != nil {
		return false, err
	}
	a.triggers[updated.ID] = updated
	a.touch(updated.Workspace)
	return true, nil
}

func (a *app) authorized(ctx context.Context, cmd pluginsdk.CommandRun, id string) (*Trigger, error) {
	a.mu.Lock()
	t, err := a.lookup(cmd, id)
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if allowed(cmd, t, nil) || allowed(cmd, t, a.parents(ctx, t.Workspace)) {
		return t, nil
	}
	return nil, fmt.Errorf("trigger %s belongs to another agent session", t.ID)
}

func (a *app) lookup(cmd pluginsdk.CommandRun, id string) (*Trigger, error) {
	t, ok := a.triggers[id]
	if !ok || t.Workspace != cmd.ProjectID {
		return nil, fmt.Errorf("unknown trigger %q", id)
	}
	return t, nil
}

func (a *app) parents(ctx context.Context, workspace string) map[string]string {
	sessions, err := a.sessions(ctx, workspace)
	if err != nil {
		return nil
	}
	return agentcall.Parents(sessions)
}

func (a *app) validateTarget(ctx context.Context, command, args string) (json.RawMessage, error) {
	if strings.HasPrefix(command, "triggers.") || strings.HasPrefix(command, "cron.") {
		return nil, fmt.Errorf("command %q cannot be run by a trigger", command)
	}
	return agentcall.ValidateTarget(ctx, a.host, command, args)
}

func allowed(cmd pluginsdk.CommandRun, t *Trigger, parents map[string]string) bool {
	if !cmd.Agent {
		return true
	}
	if cmd.Caller == "" {
		return false
	}
	return agentcall.Descends(t.Owner, cmd.Caller, parents) || agentcall.Descends(t.CreatedBy, cmd.Caller, parents)
}

func callerOf(cmd pluginsdk.CommandRun) string {
	if cmd.Agent {
		return cmd.Caller
	}
	return ""
}

func sortTriggers(triggers []*Trigger) {
	slices.SortFunc(triggers, func(x, y *Trigger) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID))
	})
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
