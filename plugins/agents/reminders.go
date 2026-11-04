package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const reminderLayout = "15:04"

type reminder struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
}

type remindersArgs struct {
	Reminders []reminder `json:"reminders"`
}

type reminders struct {
	mu   sync.Mutex
	byWS map[string]map[string]time.Time
}

func (r *reminders) set(workspace string, list []reminder) []string {
	next := map[string]time.Time{}
	for _, m := range list {
		if at, ok := next[m.Session]; !ok || m.At.Before(at) {
			next[m.Session] = m.At
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byWS == nil {
		r.byWS = map[string]map[string]time.Time{}
	}
	prev := r.byWS[workspace]
	r.byWS[workspace] = next
	var flipped []string
	for id := range prev {
		if _, ok := next[id]; !ok {
			flipped = append(flipped, id)
		}
	}
	for id := range next {
		if _, ok := prev[id]; !ok {
			flipped = append(flipped, id)
		}
	}
	return flipped
}

func (r *reminders) at(workspace, session string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.byWS[workspace][session]
	return at, ok
}

func (r *reminders) present(rec record) record {
	if rec.Status != statusDone {
		return rec
	}
	if _, ok := r.at(rec.Workspace, rec.ID); ok {
		rec.Status = statusRunning
	}
	return rec
}

func reminderActivity(at time.Time) string {
	return "next check " + at.Local().Format(reminderLayout)
}

func (a *app) remindersCommand(ctx context.Context, cmd pluginsdk.CommandRun, args remindersArgs) (any, error) {
	if cmd.Agent || cmd.Via != cronPlugin {
		return nil, fmt.Errorf("%w: %s", errUnknownCmd, cmd.ID)
	}
	flipped := a.reminders.set(cmd.WorkspaceID, args.Reminders)
	if a.host == nil {
		return nil, nil
	}
	for _, id := range flipped {
		s := a.session(id)
		if s == nil || s.snapshot().Status != statusDone {
			continue
		}
		rec := a.reminders.present(s.snapshot())
		if _, err := a.host.Publish(ctx, api.TopicChanged, api.Changed{Workspace: rec.Workspace, Session: rec.ID, Kind: api.ChangeStatus, Status: rec.Status}); err != nil {
			fmt.Fprintln(os.Stderr, "agents: publish:", err)
		}
	}
	a.workspaceState(cmd.WorkspaceID).throttle.kick()
	return nil, nil
}
