package main

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"time"

	"clubmate/plugins/internal/automation"
)

const remindersCommand = "agents.reminders"

type reminder struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
}

type remindersArgs struct {
	Reminders []reminder `json:"reminders"`
}

func (a *app) reminders(workspace string) []reminder {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []reminder{}
	for _, j := range a.jobs {
		if j.Workspace == workspace && j.Enabled && j.Schedule.OneShot() && j.Owner != "" && promptTarget(j) == j.Owner {
			out = append(out, reminder{Session: j.Owner, At: j.Schedule.At})
		}
	}
	slices.SortFunc(out, func(x, y reminder) int { return cmp.Or(x.At.Compare(y.At), cmp.Compare(x.Session, y.Session)) })
	return out
}

func (a *app) feedReminders(ctx context.Context, workspace string) {
	list := a.reminders(workspace)
	a.mu.Lock()
	prev, sent := a.sentReminders[workspace]
	a.mu.Unlock()
	if sent && slices.Equal(prev, list) || !sent && len(list) == 0 {
		return
	}
	args, err := json.Marshal(remindersArgs{Reminders: list})
	if err == nil {
		_, err = a.runCommand(ctx, automation.CommandRequest{ID: remindersCommand, WorkspaceID: workspace, Args: args})
	}
	if err != nil {
		logError(err)
		return
	}
	a.mu.Lock()
	a.sentReminders[workspace] = list
	a.mu.Unlock()
}

func (a *app) forgetReminders() {
	a.mu.Lock()
	a.sentReminders = map[string][]reminder{}
	a.mu.Unlock()
}
