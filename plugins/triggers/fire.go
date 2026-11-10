package main

import (
	"cmp"
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
	triggerNoun   = "Trigger"
	reasonForeign = "emitted by another agent session"
	maxFires      = 10
	maxLimit      = 60
	fireWindow    = time.Minute
)

func loopReason(limit int) string {
	return "fired " + strconv.Itoa(limit) + " times in " + fireWindow.String() + "; paused to stop a loop"
}

type pendingRun struct {
	trigger *Trigger
	ev      pluginsdk.Event
	refire  bool
}

type heldEvent struct {
	session string
	ev      pluginsdk.Event
}

type admitFunc func(t *Trigger, ev pluginsdk.Event, refire bool)

func (a *app) dispatch(ctx context.Context, ev pluginsdk.Event) {
	payload := decodePayload(ev.Payload)
	a.admitAll(ctx, func(admit admitFunc) {
		for id, h := range a.releaseLocked(ev.Topic, payload) {
			a.rehold(id, h, admit)
		}
		for _, t := range a.triggers {
			if t.Enabled && matches(t, ev.Topic, payload) {
				admit(t, ev, false)
			}
		}
	})
}

func (a *app) admitAll(ctx context.Context, pick func(admitFunc)) {
	now := a.now()
	var runs []pendingRun
	var tripped []*Trigger
	admit := func(t *Trigger, ev pluginsdk.Event, refire bool) {
		r := Run{At: now, Topic: ev.Topic, Seq: ev.Seq, Status: automation.StatusSkipped, Reason: automation.ReasonInFlight}
		switch {
		case a.running(t):
		case a.overLimit(t, now):
			r.Status, r.Reason = automation.StatusError, loopReason(cmp.Or(t.Limit, maxFires))
		default:
			a.inFlight[t.ID]++
			runs = append(runs, pendingRun{trigger: t.Clone(), ev: ev, refire: refire})
			return
		}
		updated := t.Clone()
		if record(updated, r) || r.Status == automation.StatusError {
			updated.Enabled = false
			updated.Paused = automation.Pause(updated.Paused, r.At, r.Reason)
			tripped = append(tripped, updated)
		}
		logError(a.put(ctx, updated))
	}
	a.mu.Lock()
	pick(admit)
	a.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	for _, r := range runs {
		a.runs.Go(func() { a.run(ctx, r) })
	}
	for _, t := range tripped {
		a.notify(ctx, t)
	}
	a.registry.Flush(ctx)
}

func (a *app) run(ctx context.Context, p pendingRun) {
	r := a.fire(ctx, p.trigger, p.ev)
	logError(a.settle(ctx, p.trigger.ID, r, p.refire))
	if r.Reason == automation.ReasonBusy {
		a.recheckHold(ctx, p.trigger)
	}
}

func (a *app) rehold(id string, h heldEvent, admit admitFunc) {
	t, ok := a.triggers[id]
	switch {
	case !ok || !t.Enabled:
	case a.inFlight[id] > 0:
		a.held[id] = h
	default:
		admit(t, h.ev, true)
	}
}

func (a *app) recheckHold(ctx context.Context, t *Trigger) {
	a.mu.Lock()
	h, ok := a.held[t.ID]
	a.mu.Unlock()
	if !ok {
		return
	}
	sessions, err := a.sessions(ctx, t.Workspace)
	if err != nil {
		logError(err)
		return
	}
	if automation.SessionBusy(sessions, h.session) {
		return
	}
	a.admitAll(ctx, func(admit admitFunc) {
		if cur, ok := a.held[t.ID]; ok && cur.same(h) {
			delete(a.held, t.ID)
			a.rehold(t.ID, h, admit)
		}
	})
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

func (a *app) running(t *Trigger) bool {
	return !t.Parallel && a.inFlight[t.ID] > 0
}

func (a *app) overLimit(t *Trigger, now time.Time) bool {
	recent := a.fired[t.ID][:0]
	for _, at := range a.fired[t.ID] {
		if now.Sub(at) < fireWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) >= cmp.Or(t.Limit, maxFires) {
		delete(a.fired, t.ID)
		return true
	}
	a.fired[t.ID] = append(recent, now)
	return false
}

func (a *app) fire(ctx context.Context, t *Trigger, ev pluginsdk.Event) Run {
	r := Run{At: a.now(), Topic: ev.Topic, Seq: ev.Seq}
	x := a.executor()
	act := t.Action()
	sessions, reason := x.Guard(ctx, act)
	r.Chain = a.chain(ctx, t.Workspace, ev, sessions)
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

func (h heldEvent) same(o heldEvent) bool {
	return h.session == o.session && h.ev.Topic == o.ev.Topic && h.ev.Seq == o.ev.Seq
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
	if a.inFlight[id]--; a.inFlight[id] <= 0 {
		delete(a.inFlight, id)
	}
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
	switch {
	case paused:
		a.notify(ctx, updated)
	case updated.Run == "":
		a.registry.NotifyRun(ctx, triggerNoun, updated.Action(), r)
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
	a.registry.NotifyPaused(ctx, triggerNoun+" paused", t.Action(), t.LastError)
}

func (a *app) executor() automation.Executor {
	return automation.Executor{Origin: automation.OriginTrigger, Sessions: a.sessions, RunCommand: a.runCommand, Log: logError}
}

func record(t *Trigger, r Run) (paused bool) {
	paused = automation.Record(&t.History, &t.Failures, &t.LastError, r)
	t.Paused = automation.KeepSpans(t.Paused, t.History)
	if paused {
		t.Enabled = false
		t.Paused = automation.Pause(t.Paused, r.At, t.LastError)
	}
	return paused
}

func logError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "triggers:", err)
	}
}
