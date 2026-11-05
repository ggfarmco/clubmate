package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/apns"
	"clubmate/pkg/pluginsdk"
)

const (
	notifyTopic        = "notify.posted"
	agentsChangedTopic = "agents.changed"
	changeResolved     = "resolved"
	requestPermission  = "permission"
	agentSessionPrefix = "agt_"
	sessionsCommand    = "agents.sessions"
	notifyPostCommand  = "notify.post"
	pushMaxAge         = time.Hour
	relayAlertEvery    = time.Hour
	freePushKind       = "info"
	alertSound         = "default"
	passiveLevel       = "passive"
	stuckKind          = "stuck"
	relayAlertTitle    = "Push relay unreachable"

	summaryAccessOff    = "companion access off"
	summaryNoRelay      = "no relay set"
	summaryStale        = "held: older than 1h"
	summaryMuted        = "held: session muted"
	summaryNeedsInput   = "held: session needs-input only"
	summaryNoToken      = "no device with a token"
	outcomeStreamOpen   = "skipped, stream open"
	outcomeUnreachable  = "relay unreachable"
	outcomeTokenDropped = ", token dropped"
	summarySeparator    = " · "

	policyNeedsInput = "needs-input"
	policyMuted      = "muted"
)

var (
	pushKinds = map[string]string{
		"needs-input": "Needs input",
		"permission":  "Permission needed",
		"question":    "Question",
		"done":        "Done",
		"stuck":       "Stuck",
	}
	needsInputKinds = []string{"needs-input", "permission", "question"}

	errNoPushMessage   = errors.New("remote.push needs notification or message")
	errBadNotification = errors.New("notification must be JSON text of a notify.posted payload with an id")
)

type pushArgs struct {
	Notification string `json:"notification"`
	Title        string `json:"title"`
	Message      string `json:"message"`
	Session      string `json:"session"`
}

type pushResult struct {
	Summary string       `json:"summary"`
	Devices []devicePush `json:"devices,omitempty"`
}

type devicePush struct {
	Device  string `json:"device"`
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	err     error
}

type relayAlert struct {
	mu   sync.Mutex
	last time.Time
}

type notifyPost struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type pushLog struct {
	mu   sync.Mutex
	sent map[pushedKey]time.Time
}

type pushedKey struct {
	device       string
	notification string
}

type pushTokenRequest struct {
	Token       string `json:"token"`
	Environment string `json:"environment"`
}

type pushTokenResult struct {
	Push bool `json:"push"`
}

type notifyPayload struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Collapse  string `json:"collapse"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
	Quiet     bool   `json:"quiet"`
	TS        string `json:"ts"`
}

func (n notifyPayload) collapseID() string {
	if n.Collapse != "" && n.Collapse == n.Session {
		return n.Collapse
	}
	return n.ID
}

type agentsChanged struct {
	Session     string `json:"session"`
	Kind        string `json:"kind"`
	RequestKind string `json:"requestKind"`
}

type sessionPolicy struct {
	ID              string `json:"id"`
	NotifyEffective string `json:"notifyEffective"`
}

type sessionArgs struct {
	Session string `json:"session"`
}

type alertBody struct {
	APS       alertAPS `json:"aps"`
	ID        string   `json:"id"`
	Computer  string   `json:"computer"`
	Workspace string   `json:"workspace"`
	Session   string   `json:"session,omitempty"`
	Kind      string   `json:"kind"`
}

type alertAPS struct {
	Alert        alert  `json:"alert"`
	Sound        string `json:"sound,omitempty"`
	Interruption string `json:"interruption-level,omitempty"`
	ThreadID     string `json:"thread-id"`
}

type alert struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
}

type backgroundBody struct {
	APS      backgroundAPS `json:"aps"`
	Resolved resolvedPush  `json:"resolved"`
}

type backgroundAPS struct {
	ContentAvailable int `json:"content-available"`
}

type resolvedPush struct {
	Computer    string `json:"computer"`
	Session     string `json:"session"`
	RequestKind string `json:"requestKind"`
}

func (s *server) pushToken(ctx context.Context, d device, p pushTokenRequest) (pushTokenResult, error) {
	if p.Token == "" {
		p.Environment = ""
	} else if !apns.ValidDeviceToken(p.Token) || p.Environment != apns.EnvDevelopment && p.Environment != apns.EnvProduction {
		return pushTokenResult{}, &apiError{Code: codeInvalidParams, Message: "token must be hex, at most 200 characters; environment development or production"}
	}
	if err := s.devices.setPush(ctx, d.ID, p.Token, p.Environment); err != nil {
		return pushTokenResult{}, err
	}
	return pushTokenResult{Push: s.relay.configured()}, nil
}

func (s *server) push(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic != agentsChangedTopic || !s.relay.configured() {
		return
	}
	s.pushResolved(ctx, ev)
}

func (s *server) pushCommand(ctx context.Context, workspace string, enabled bool, a pushArgs) (pushResult, error) {
	var n notifyPayload
	switch {
	case a.Notification != "":
		if err := json.Unmarshal([]byte(a.Notification), &n); err != nil || n.ID == "" {
			return pushResult{}, errBadNotification
		}
		workspace = cmp.Or(n.Workspace, workspace)
	case a.Message != "":
		n = notifyPayload{Title: a.Title, Message: a.Message, Session: a.Session, Kind: freePushKind}
	default:
		return pushResult{}, errNoPushMessage
	}
	if held := s.held(ctx, workspace, enabled, n); held != "" {
		return pushResult{Summary: held}, nil
	}
	comp := s.identity.get()
	id := n.ID
	if id == "" {
		id = rand.Text()
	}
	aps := alertAPS{
		Alert:    alert{Title: s.alertTitle(ctx, comp.Name, workspace), Subtitle: cmp.Or(n.Title, kindLabel(n.Kind)), Body: n.Message},
		Sound:    alertSound,
		ThreadID: comp.ID,
	}
	if n.Quiet {
		aps.Sound, aps.Interruption = "", passiveLevel
	}
	body, err := json.Marshal(alertBody{
		APS:       aps,
		ID:        comp.ID + "|" + id,
		Computer:  comp.ID,
		Workspace: workspace,
		Session:   n.Session,
		Kind:      n.Kind,
	})
	if err != nil {
		return pushResult{}, err
	}
	devices := s.pushAll(ctx, relayPush{pushType: apns.TypeAlert, collapseID: truncate(n.collapseID(), apns.MaxCollapseID), notification: n.ID, payload: body})
	if i := slices.IndexFunc(devices, func(d devicePush) bool { return d.err != nil }); i >= 0 {
		s.alertRelayDown(ctx, workspace, devices[i].err)
	}
	return pushResult{Summary: summary(devices), Devices: devices}, nil
}

func (s *server) held(ctx context.Context, workspace string, enabled bool, n notifyPayload) string {
	switch {
	case !enabled:
		return summaryAccessOff
	case !s.relay.configured():
		return summaryNoRelay
	case s.stale(n.TS):
		return summaryStale
	}
	if policy := s.policy(ctx, workspace, n.Session); !allows(policy, n.Kind) {
		if policy == policyMuted {
			return summaryMuted
		}
		return summaryNeedsInput
	}
	if !slices.ContainsFunc(s.devices.list(), func(d device) bool { return d.PushToken != "" }) {
		return summaryNoToken
	}
	return ""
}

func (s *server) alertRelayDown(ctx context.Context, workspace string, cause error) {
	if !s.relayAlert.due(s.now()) {
		return
	}
	args, err := json.Marshal(notifyPost{Kind: stuckKind, Title: relayAlertTitle, Message: cause.Error()})
	if err == nil {
		_, err = s.backend.Run(ctx, notifyPostCommand, workspace, args)
	}
	if err != nil {
		logf("post relay alert: %v", err)
	}
}

func (r *relayAlert) due(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.last.IsZero() && now.Sub(r.last) < relayAlertEvery {
		return false
	}
	r.last = now
	return true
}

func (s *server) pushResolved(ctx context.Context, ev pluginsdk.Event) {
	var c agentsChanged
	if err := json.Unmarshal(ev.Payload, &c); err != nil || c.Kind != changeResolved {
		return
	}
	if c.RequestKind == "" {
		c.RequestKind = requestPermission
	}
	body, err := json.Marshal(backgroundBody{
		APS:      backgroundAPS{ContentAvailable: 1},
		Resolved: resolvedPush{Computer: s.identity.get().ID, Session: c.Session, RequestKind: c.RequestKind},
	})
	if err != nil {
		return
	}
	s.pushAll(ctx, relayPush{pushType: apns.TypeBackground, payload: body})
}

func (s *server) pushAll(ctx context.Context, p relayPush) []devicePush {
	var out []devicePush
	for _, d := range s.devices.list() {
		if d.PushToken == "" {
			continue
		}
		r := devicePush{Device: d.ID, Name: d.Name}
		if s.hub.connected(d.ID) {
			r.Outcome = outcomeStreamOpen
			out = append(out, r)
			continue
		}
		s.pushed.add(d.ID, p.notification)
		reply, err := s.relay.send(ctx, d.PushEnvironment, d.PushToken, p)
		if err != nil || reply.Status != http.StatusOK {
			s.pushed.forget(d.ID, p.notification)
		}
		switch {
		case err != nil:
			logf("push to %s: %v", d.ID, err)
			r.Outcome, r.err = outcomeUnreachable, err
		case reply.GoneToken():
			r.Outcome = replyWords(reply) + outcomeTokenDropped
			if err := s.devices.dropPush(ctx, d.PushToken); err != nil {
				logf("drop push token of %s: %v", d.ID, err)
			}
		default:
			r.Outcome = replyWords(reply)
			if reply.Status != http.StatusOK {
				logf("push to %s: %d %s", d.ID, reply.Status, reply.Reason)
			}
		}
		out = append(out, r)
	}
	return out
}

func (s *server) replayed(device string, ev pluginsdk.Event) pluginsdk.Event {
	if ev.Topic != notifyTopic {
		return ev
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return ev
	}
	var id string
	if err := json.Unmarshal(payload["id"], &id); err != nil || !s.pushed.has(device, id) {
		return ev
	}
	payload["pushed"] = json.RawMessage("true")
	raw, err := json.Marshal(payload)
	if err != nil {
		return ev
	}
	ev.Payload = raw
	return ev
}

func (l *pushLog) add(device, notification string) {
	if notification == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.sent == nil {
		l.sent = map[pushedKey]time.Time{}
	}
	maps.DeleteFunc(l.sent, func(_ pushedKey, at time.Time) bool { return now.Sub(at) > pushMaxAge })
	l.sent[pushedKey{device, notification}] = now
}

func (l *pushLog) forget(device, notification string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sent, pushedKey{device, notification})
}

func (l *pushLog) has(device, notification string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.sent[pushedKey{device, notification}]
	return ok
}

func (s *server) policy(ctx context.Context, workspace, session string) string {
	if !strings.HasPrefix(session, agentSessionPrefix) {
		return ""
	}
	args, err := json.Marshal(sessionArgs{Session: session})
	if err != nil {
		return ""
	}
	out, err := s.backend.Run(ctx, sessionsCommand, workspace, args)
	if err != nil {
		return ""
	}
	var sessions []sessionPolicy
	if err := json.Unmarshal(out, &sessions); err != nil {
		return ""
	}
	i := slices.IndexFunc(sessions, func(p sessionPolicy) bool { return p.ID == session })
	if i < 0 {
		return ""
	}
	return sessions[i].NotifyEffective
}

func (s *server) alertTitle(ctx context.Context, mac, workspace string) string {
	parts := []string{mac}
	if list, err := s.backend.Workspaces(ctx); err == nil {
		if i := slices.IndexFunc(list, func(w pluginsdk.Workspace) bool { return w.ID == workspace }); i >= 0 {
			parts = append(parts, list[i].Name)
		}
	}
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " · ")
}

func allows(policy, kind string) bool {
	switch policy {
	case policyMuted:
		return false
	case policyNeedsInput:
		return slices.Contains(needsInputKinds, kind)
	}
	return true
}

func (s *server) stale(ts string) bool {
	t, err := time.Parse(time.RFC3339, ts)
	return err == nil && s.now().Sub(t) > pushMaxAge
}

func summary(devices []devicePush) string {
	parts := make([]string, 0, len(devices))
	for _, d := range devices {
		parts = append(parts, d.Name+" "+d.Outcome)
	}
	return strings.Join(parts, summarySeparator)
}

func replyWords(r apns.Reply) string {
	return strings.TrimSpace(strconv.Itoa(r.Status) + " " + r.Reason)
}

func kindLabel(kind string) string {
	if label, ok := pushKinds[kind]; ok {
		return label
	}
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
