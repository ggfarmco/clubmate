package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	sessionField  = "session"
	reasonForeign = "emitted by another agent session"
	maxFires      = 10
	fireWindow    = time.Minute
)

var reasonLoop = "fired " + strconv.Itoa(maxFires) + " times in " + fireWindow.String() + "; paused to stop a loop"

type pendingRun struct {
	trigger *Trigger
	ev      pluginsdk.Event
	refire  bool
}

type heldEvent struct {
	session string
	ev      pluginsdk.Event
}

func (a *app) dispatch(ctx context.Context, ev pluginsdk.Event) {
	payload := decodePayload(ev.Payload)
	now := a.now()
	var runs []pendingRun
	var tripped []*Trigger
	admit := func(t *Trigger, ev pluginsdk.Event, refire bool) {
		r := Run{At: now, Topic: ev.Topic, Seq: ev.Seq, Status: automation.StatusSkipped, Reason: automation.ReasonInFlight}
		switch {
		case a.inFlight[t.ID]:
		case a.overLimit(t.ID, now):
			r.Status, r.Reason = automation.StatusError, reasonLoop
		default:
			a.inFlight[t.ID] = true
			runs = append(runs, pendingRun{trigger: t.Clone(), ev: ev, refire: refire})
			return
		}
		updated := t.Clone()
		if record(updated, r) || r.Reason == reasonLoop {
			updated.Enabled = false
			tripped = append(tripped, updated)
		}
		logError(a.put(ctx, updated))
	}
	a.mu.Lock()
	for id, h := range a.releaseLocked(ev.Topic, payload) {
		t, ok := a.triggers[id]
		switch {
		case !ok || !t.Enabled:
		case a.inFlight[id]:
			a.held[id] = h
		default:
			admit(t, h.ev, true)
		}
	}
	for _, t := range a.triggers {
		if t.Enabled && matches(t, ev.Topic, payload) {
			admit(t, ev, false)
		}
	}
	a.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	for _, r := range runs {
		a.runs.Go(func() { logError(a.settle(ctx, r.trigger.ID, a.fire(ctx, r.trigger, r.ev), r.refire)) })
	}
	for _, t := range tripped {
		a.notify(ctx, t)
	}
	a.registry.Flush(ctx)
}

func (a *app) releaseLocked(topic string, payload any) map[string]heldEvent {
	if topic != api.TopicTurn {
		return nil
	}
	v, _ := lookup(payload, sessionField)
	session, _ := v.(string)
	released := map[string]heldEvent{}
	for id, h := range a.held {
		if h.session == session {
			released[id] = h
			delete(a.held, id)
		}
	}
	return released
}

func (a *app) unhold(session string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	maps.DeleteFunc(a.held, func(_ string, h heldEvent) bool { return h.session == session })
}

func (a *app) overLimit(id string, now time.Time) bool {
	recent := a.fired[id][:0]
	for _, at := range a.fired[id] {
		if now.Sub(at) < fireWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) >= maxFires {
		delete(a.fired, id)
		return true
	}
	a.fired[id] = append(recent, now)
	return false
}

func (a *app) fire(ctx context.Context, t *Trigger, ev pluginsdk.Event) Run {
	r := Run{At: a.now(), Topic: ev.Topic, Seq: ev.Seq}
	x := a.executor()
	act := t.Action()
	sessions, reason := x.Guard(ctx, act)
	if reason == "" && foreign(t, ev, sessions) {
		reason = reasonForeign
	}
	if reason != "" {
		r.Status, r.Reason = automation.StatusSkipped, reason
		return r
	}
	args, err := expand(t.Args, ev)
	if err != nil {
		r.Status, r.Reason = automation.StatusError, err.Error()
		return r
	}
	act.Args = args
	if target, busy := automation.BusyTarget(sessions, act); busy {
		a.mu.Lock()
		a.held[t.ID] = heldEvent{session: target, ev: ev}
		a.mu.Unlock()
		r.Status, r.Reason = automation.StatusSkipped, automation.ReasonBusy
		return r
	}
	return x.Execute(ctx, act, r)
}

func foreign(t *Trigger, ev pluginsdk.Event, sessions []api.Session) bool {
	if t.Owner == "" || !strings.HasPrefix(ev.Topic, emitPrefix) || !placeholderRe.Match(t.Args) {
		return false
	}
	by, _ := lookup(decodePayload(ev.Payload), byField)
	session, _ := by.(string)
	return session != "" && !agentcall.Descends(session, t.Owner, agentcall.Parents(sessions))
}

func (a *app) settle(ctx context.Context, id string, r Run, refire bool) error {
	a.mu.Lock()
	delete(a.inFlight, id)
	current, ok := a.triggers[id]
	if !ok || refire && r.Reason == automation.ReasonBusy {
		a.mu.Unlock()
		return nil
	}
	updated := current.Clone()
	paused := record(updated, r)
	err := a.put(ctx, updated)
	a.mu.Unlock()
	a.registry.Flush(ctx)
	if paused {
		a.notify(ctx, updated)
	}
	return err
}

func (a *app) put(ctx context.Context, t *Trigger) error {
	a.triggers[t.ID] = t
	a.registry.Touch(t.Workspace)
	return a.store.Save(ctx, t)
}

func (a *app) drop(ctx context.Context, t *Trigger) error {
	delete(a.triggers, t.ID)
	delete(a.fired, t.ID)
	delete(a.held, t.ID)
	a.registry.Touch(t.Workspace)
	return a.store.Remove(ctx, t)
}

func (a *app) notifyPaused(ctx context.Context, t *Trigger) {
	a.registry.NotifyPaused(ctx, a.host, "Trigger paused", t.Action(), t.LastError)
}

func (a *app) executor() automation.Executor {
	return automation.Executor{Sessions: a.sessions, RunCommand: a.runCommand, Log: logError}
}

func record(t *Trigger, r Run) (paused bool) {
	if !automation.Record(&t.History, &t.Failures, &t.LastError, r) {
		return false
	}
	t.Enabled = false
	return true
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "triggers:", err)
	}
}
