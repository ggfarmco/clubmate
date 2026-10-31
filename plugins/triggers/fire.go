package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
)

const (
	promptCommand  = "agents.prompt"
	turnTopic      = "agents.turn"
	sessionField   = "session"
	sessionRunning = "running"
	sessionWaiting = "waiting"
	noApprover     = "no app to approve"
	reasonBusy     = "busy"
	reasonNoApp    = "no app"
	reasonInFlight = "previous run still running"
	reasonGone     = "owner gone"
	reasonUnknown  = "owner unknown"
	reasonForeign  = "emitted by another agent session"
	maxHistory     = 20
	maxFailures    = 3
	maxResult      = 200
	maxFires       = 10
	fireWindow     = time.Minute
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

type promptTarget struct {
	Session string `json:"session"`
}

type notifyRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}

func (a *app) dispatch(ctx context.Context, ev pluginsdk.Event) {
	payload := decodePayload(ev.Payload)
	now := a.now()
	var runs []pendingRun
	var tripped []*Trigger
	admit := func(t *Trigger, ev pluginsdk.Event, refire bool) {
		r := Run{At: now, Topic: ev.Topic, Seq: ev.Seq, Status: statusSkipped, Reason: reasonInFlight}
		switch {
		case a.inFlight[t.ID]:
		case a.overLimit(t.ID, now):
			r.Status, r.Reason = statusError, reasonLoop
		default:
			a.inFlight[t.ID] = true
			runs = append(runs, pendingRun{trigger: clone(t), ev: ev, refire: refire})
			return
		}
		updated := clone(t)
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
	a.flush(ctx)
}

func (a *app) releaseLocked(topic string, payload any) map[string]heldEvent {
	if topic != turnTopic {
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
	var sessions []agentcall.Session
	var err error
	if t.Owner != "" || t.Command == promptCommand {
		if sessions, err = a.sessions(ctx, t.Workspace); err != nil {
			logError(err)
			if t.Owner != "" {
				r.Status, r.Reason = statusSkipped, reasonUnknown
				return r
			}
		}
	}
	if s, ok := agentcall.Find(sessions, t.Owner); t.Owner != "" && (!ok || s.Closed) {
		r.Status, r.Reason = statusSkipped, reasonGone
		return r
	}
	if foreign(t, ev, sessions) {
		r.Status, r.Reason = statusSkipped, reasonForeign
		return r
	}
	args, err := expand(t.Args, ev)
	if err != nil {
		r.Status, r.Reason = statusError, err.Error()
		return r
	}
	if t.Command == promptCommand {
		var target promptTarget
		if json.Unmarshal(args, &target) == nil {
			if s, ok := agentcall.Find(sessions, target.Session); ok && (s.Status == sessionRunning || s.Status == sessionWaiting) {
				a.mu.Lock()
				a.held[t.ID] = heldEvent{session: target.Session, ev: ev}
				a.mu.Unlock()
				r.Status, r.Reason = statusSkipped, reasonBusy
				return r
			}
		}
	}
	raw, err := a.runCommand(ctx, commandRequest{ID: t.Command, ProjectID: t.Workspace, Args: args, Agent: true, Caller: t.Owner})
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

func foreign(t *Trigger, ev pluginsdk.Event, sessions []agentcall.Session) bool {
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
	if !ok || refire && r.Reason == reasonBusy {
		a.mu.Unlock()
		return nil
	}
	updated := clone(current)
	paused := record(updated, r)
	if paused {
		updated.Enabled = false
	}
	err := a.put(ctx, updated)
	a.mu.Unlock()
	a.flush(ctx)
	if paused {
		a.notify(ctx, updated)
	}
	return err
}

func (a *app) put(ctx context.Context, t *Trigger) error {
	a.triggers[t.ID] = t
	a.touch(t.Workspace)
	return a.store.save(ctx, t)
}

func record(t *Trigger, r Run) (paused bool) {
	t.History = append(t.History, r)
	if extra := len(t.History) - maxHistory; extra > 0 {
		t.History = t.History[extra:]
	}
	switch r.Status {
	case statusOK:
		t.Failures = 0
		t.LastError = ""
	case statusError:
		t.Failures++
		t.LastError = r.Reason
		return t.Failures >= maxFailures
	}
	return false
}

func (a *app) notifyPaused(ctx context.Context, t *Trigger) {
	req := notifyRequest{ProjectID: t.Workspace, Title: "Trigger paused", Message: t.Name + ": " + t.LastError, Kind: "stuck"}
	if err := a.host.Call(ctx, "notify.post", req, nil); err != nil {
		logError(fmt.Errorf("notify paused trigger %s: %w", t.ID, err))
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
