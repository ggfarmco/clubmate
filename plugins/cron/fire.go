package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"clubmate/plugins/internal/agentcall"
)

const (
	promptCommand  = "agents.prompt"
	sessionRunning = "running"
	sessionWaiting = "waiting"
	noApprover     = "no app to approve"
	reasonBusy     = "busy"
	reasonNoApp    = "no app"
	reasonMissed   = "missed"
	reasonInFlight = "previous run still running"
	reasonGone     = "owner gone"
	reasonUnknown  = "owner unknown"
)

type promptTarget struct {
	Session string `json:"session"`
}

type notifyRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}

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
		updated := clone(j)
		start := false
		switch {
		case d == skipMissed:
			record(updated, Run{At: now, Status: statusSkipped, Reason: reasonMissed})
		case busy:
			record(updated, Run{At: now, Status: statusSkipped, Reason: reasonInFlight})
		default:
			start = true
		}
		keep := advance(updated, now)
		if start && !keep {
			if err := a.store.remove(ctx, updated); err != nil {
				logError(err)
				continue
			}
		}
		if start {
			a.inFlight[id] = true
			job, spent := clone(updated), !keep
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
	r := Run{At: a.now()}
	var sessions []agentSession
	if j.Owner != "" || j.Command == promptCommand {
		var err error
		if sessions, err = a.sessions(ctx, j.Workspace); err != nil {
			logError(err)
			if j.Owner != "" {
				r.Status, r.Reason = statusSkipped, reasonUnknown
				return r
			}
		}
	}
	if s, ok := agentcall.Find(sessions, j.Owner); j.Owner != "" && (!ok || s.Closed) {
		r.Status, r.Reason = statusSkipped, reasonGone
		return r
	}
	if j.Command == promptCommand {
		var target promptTarget
		if json.Unmarshal(j.Args, &target) == nil {
			if s, ok := agentcall.Find(sessions, target.Session); ok && (s.Status == sessionRunning || s.Status == sessionWaiting) {
				r.Status, r.Reason = statusSkipped, reasonBusy
				return r
			}
		}
	}
	raw, err := a.runCommand(ctx, commandRequest{ID: j.Command, ProjectID: j.Workspace, Args: j.Args, Agent: true, Caller: j.Owner})
	switch {
	case err == nil:
		r.Status, r.Result = statusOK, truncate(string(raw), maxResult)
	case strings.Contains(err.Error(), noApprover):
		r.Status, r.Reason = statusSkipped, reasonNoApp
	default:
		r.Status, r.Reason = statusError, err.Error()
	}
	return r
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
	a.flush(ctx)
	if paused {
		a.notifyPaused(ctx, j)
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
	updated := clone(current)
	paused := record(updated, r)
	if _, more := updated.Schedule.Next(r.At); spent && !more {
		if r.Status != statusSkipped {
			return updated, paused, a.drop(ctx, updated)
		}
		updated.NextRun = a.now().Add(tick)
	}
	if err := a.store.save(ctx, updated); err != nil {
		return nil, false, err
	}
	a.jobs[id] = updated
	a.touch(updated.Workspace)
	return updated, paused, nil
}

func (a *app) put(ctx context.Context, j *Job) error {
	a.jobs[j.ID] = j
	a.touch(j.Workspace)
	return a.store.save(ctx, j)
}

func (a *app) drop(ctx context.Context, j *Job) error {
	delete(a.jobs, j.ID)
	a.touch(j.Workspace)
	return a.store.remove(ctx, j)
}

func (a *app) notifyPaused(ctx context.Context, j *Job) {
	req := notifyRequest{ProjectID: j.Workspace, Title: "Cron job paused", Message: j.Name + ": " + j.LastError, Kind: "stuck"}
	if err := a.host.Call(ctx, "notify.post", req, nil); err != nil {
		logError(fmt.Errorf("notify paused job %s: %w", j.ID, err))
	}
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "cron:", err)
	}
}
