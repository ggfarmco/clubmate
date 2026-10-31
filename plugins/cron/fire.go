package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"clubmate/plugins/internal/automation"
)

const reasonMissed = "missed"

func (a *app) tick(ctx context.Context) time.Duration {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, j := range a.jobs {
		d := decide(j, now)
		busy := a.inFlight[id]
		if d == wait || busy && j.Schedule.OneShot() {
			continue
		}
		updated := j.Clone()
		start := false
		switch {
		case d == skipMissed:
			record(updated, Run{At: now, Status: automation.StatusSkipped, Reason: reasonMissed})
		case busy:
			record(updated, Run{At: now, Status: automation.StatusSkipped, Reason: automation.ReasonInFlight})
		default:
			start = true
		}
		keep := advance(updated, now)
		if start && !keep {
			if err := a.store.Remove(ctx, updated); err != nil {
				logError(err)
				continue
			}
		}
		if start {
			a.inFlight[id] = true
			job, spent := updated.Clone(), !keep
			a.runs.Go(func() { logError(a.settle(ctx, id, a.fire(ctx, job), spent)) })
		}
		switch {
		case keep:
			logError(a.put(ctx, updated))
		case !start:
			logError(a.drop(ctx, updated))
		}
	}
	jobs := make([]*Job, 0, len(a.jobs))
	for id, j := range a.jobs {
		if !a.inFlight[id] || !j.Schedule.OneShot() {
			jobs = append(jobs, j)
		}
	}
	return wakeAfter(jobs, now)
}

func (a *app) fire(ctx context.Context, j *Job) Run {
	return a.executor().Fire(ctx, j.Action(), Run{At: a.now()})
}

func (a *app) claim(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inFlight[id] {
		return false
	}
	a.inFlight[id] = true
	return true
}

func (a *app) settle(ctx context.Context, id string, r Run, spent bool) error {
	a.mu.Lock()
	delete(a.inFlight, id)
	j, paused, err := a.recordLocked(ctx, id, r, spent)
	a.mu.Unlock()
	a.wake()
	a.registry.Flush(ctx)
	if paused {
		a.registry.NotifyPaused(ctx, a.host, "Cron job paused", j.Action(), j.LastError)
	}
	return err
}

func (a *app) note(ctx context.Context, id string, r Run) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _, err := a.recordLocked(ctx, id, r, false)
	return err
}

func (a *app) recordLocked(ctx context.Context, id string, r Run, spent bool) (*Job, bool, error) {
	current, ok := a.jobs[id]
	if !ok {
		return nil, false, nil
	}
	updated := current.Clone()
	paused := record(updated, r)
	if _, more := updated.Schedule.Next(r.At); spent && !more {
		if r.Status != automation.StatusSkipped {
			return updated, paused, a.drop(ctx, updated)
		}
		updated.NextRun = a.now().Add(tick)
	}
	if err := a.store.Save(ctx, updated); err != nil {
		return nil, false, err
	}
	a.jobs[id] = updated
	a.registry.Touch(updated.Workspace)
	return updated, paused, nil
}

func (a *app) put(ctx context.Context, j *Job) error {
	a.jobs[j.ID] = j
	a.registry.Touch(j.Workspace)
	return a.store.Save(ctx, j)
}

func (a *app) drop(ctx context.Context, j *Job) error {
	delete(a.jobs, j.ID)
	a.registry.Touch(j.Workspace)
	return a.store.Remove(ctx, j)
}

func (a *app) executor() automation.Executor {
	return automation.Executor{Sessions: a.sessions, RunCommand: a.runCommand, Log: logError}
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "cron:", err)
	}
}
