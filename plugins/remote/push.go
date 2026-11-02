package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	notifyTopic        = "notify.posted"
	agentsChangedTopic = "agents.changed"
	changeResolved     = "resolved"
	requestPermission  = "permission"
	agentSessionPrefix = "agt_"
	sessionsCommand    = "agents.sessions"
	maxPushToken       = 200
	maxCollapseID      = 64
	pushMaxAge         = time.Hour

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
)

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
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
	TS        string `json:"ts"`
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
	Alert    alert  `json:"alert"`
	Sound    string `json:"sound"`
	ThreadID string `json:"thread-id"`
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
	} else if !validPushToken(p.Token) || p.Environment != envDevelopment && p.Environment != envProduction {
		return pushTokenResult{}, &apiError{Code: codeInvalidParams, Message: "token must be hex, at most 200 characters; environment development or production"}
	}
	if err := s.devices.setPush(ctx, d.ID, p.Token, p.Environment); err != nil {
		return pushTokenResult{}, err
	}
	return pushTokenResult{Push: s.apns.configured()}, nil
}

func (s *server) push(ctx context.Context, ev pluginsdk.Event) {
	if !s.apns.configured() {
		return
	}
	switch ev.Topic {
	case notifyTopic:
		s.pushNotification(ctx, ev)
	case agentsChangedTopic:
		s.pushResolved(ctx, ev)
	}
}

func (s *server) pushNotification(ctx context.Context, ev pluginsdk.Event) {
	var n notifyPayload
	if err := json.Unmarshal(ev.Payload, &n); err != nil || n.ID == "" {
		return
	}
	label, ok := pushKinds[n.Kind]
	if !ok || stale(n.TS) {
		return
	}
	workspace := n.Workspace
	if workspace == "" {
		workspace = ev.Workspace
	}
	if !allows(s.policy(ctx, workspace, n.Session), n.Kind) {
		return
	}
	comp := s.identity.get()
	subtitle := n.Title
	if subtitle == "" {
		subtitle = label
	}
	body, err := json.Marshal(alertBody{
		APS: alertAPS{
			Alert:    alert{Title: s.alertTitle(ctx, comp.Name, workspace), Subtitle: subtitle, Body: n.Message},
			Sound:    "default",
			ThreadID: comp.ID,
		},
		ID:        comp.ID + "|" + n.ID,
		Computer:  comp.ID,
		Workspace: workspace,
		Session:   n.Session,
		Kind:      n.Kind,
	})
	if err != nil {
		return
	}
	s.pushAll(ctx, apnsPush{pushType: "alert", priority: "10", collapseID: truncate(n.ID, maxCollapseID), body: body})
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
	s.pushAll(ctx, apnsPush{pushType: "background", priority: "5", body: body})
}

func (s *server) pushAll(ctx context.Context, p apnsPush) {
	for _, d := range s.devices.list() {
		if d.PushToken == "" || s.hub.connected(d.ID) {
			continue
		}
		reply, err := s.apns.send(ctx, d.PushEnvironment, d.PushToken, p)
		if err != nil {
			logf("push to %s: %v", d.ID, err)
			continue
		}
		if reply.goneToken() {
			if err := s.devices.dropPush(ctx, d.PushToken); err != nil {
				logf("drop push token of %s: %v", d.ID, err)
			}
			continue
		}
		if reply.status != http.StatusOK {
			logf("push to %s: %d %s", d.ID, reply.status, reply.reason)
		}
	}
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

func stale(ts string) bool {
	t, err := time.Parse(time.RFC3339, ts)
	return err == nil && time.Since(t) > pushMaxAge
}

func validPushToken(token string) bool {
	if len(token) > maxPushToken {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
