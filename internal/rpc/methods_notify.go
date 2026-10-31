package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"clubmate/internal/notify"
	"clubmate/internal/session"
	"clubmate/pkg/id"
)

const (
	notifyPrefix      = "ntf"
	notifyRateMax     = 5
	notifyRateTotal   = 20
	notifyRateWindow  = 10 * time.Second
	notifyMethod      = "notify.post"
	notifyTimeLayout  = time.RFC3339Nano
	notifyAgentSource = "agent"
)

var notifySessionPattern = regexp.MustCompile(`^[a-z]{2,8}_[0-9a-f]{16}$`)

type notifyParams struct {
	ProjectID string `json:"projectId"`
	Session   string `json:"session"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}

type notifyPayload struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Session   string `json:"session,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
	Agent     bool   `json:"agent,omitempty"`
	TS        string `json:"ts"`
}

type notifyResult struct {
	ID string `json:"id"`
}

func (s *Server) notifyMethods() map[string]handlerFunc {
	return map[string]handlerFunc{notifyMethod: method(s.notifyPost)}
}

func (s *Server) notifyPost(ctx context.Context, p notifyParams) (notifyResult, error) {
	if s.bus == nil {
		return notifyResult{}, errNoBus
	}
	if err := s.requireProject(ctx, p.ProjectID); err != nil {
		return notifyResult{}, err
	}
	payload, err := s.notifyPayload(ctx, p)
	if err != nil {
		return notifyResult{}, err
	}
	if !s.notifyLimit.Allow(p.ProjectID, time.Now()) {
		return notifyResult{}, fmt.Errorf("%w: too many notifications (at most %d per %s per workspace, %d in all)", ErrRateLimited, notifyRateMax, notifyRateWindow, notifyRateTotal)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return notifyResult{}, err
	}
	source := CallerFrom(ctx).String()
	if payload.Agent {
		source = notifyAgentSource
	}
	if _, err := s.bus.Publish(ctx, notify.Topic, source, raw); err != nil {
		return notifyResult{}, err
	}
	return notifyResult{ID: payload.ID}, nil
}

func (s *Server) notifyPayload(ctx context.Context, p notifyParams) (notifyPayload, error) {
	kind := p.Kind
	if kind == "" {
		kind = notify.DefaultKind
	}
	if !notify.ValidKind(kind) {
		return notifyPayload{}, invalidParams(fmt.Sprintf("unknown kind %q (want one of %s)", kind, strings.Join(notify.Kinds, ", ")))
	}
	message := notify.Clip(p.Message, notify.MaxMessage)
	if message == "" {
		return notifyPayload{}, invalidParams("message is required")
	}
	owner, err := s.notifyOwner(ctx, p.ProjectID, p.Session)
	if err != nil {
		return notifyPayload{}, err
	}
	return notifyPayload{
		ID:        id.New(notifyPrefix),
		Workspace: p.ProjectID,
		Session:   p.Session,
		Owner:     owner,
		Title:     notify.Clip(p.Title, notify.MaxTitle),
		Message:   message,
		Kind:      kind,
		Agent:     isAgentCall(ctx),
		TS:        time.Now().UTC().Format(notifyTimeLayout),
	}, nil
}

func (s *Server) notifyOwner(ctx context.Context, projectID, sessionID string) (string, error) {
	switch {
	case sessionID == "":
		return "", nil
	case id.Valid(session.IDPrefix, sessionID):
		got, err := s.svc.GetSession(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if got.ProjectID != projectID {
			return "", invalidParams(fmt.Sprintf("session %q is not in workspace %q", sessionID, projectID))
		}
		return got.Owner, nil
	case notifySessionPattern.MatchString(sessionID):
		return "", nil
	}
	return "", invalidParams(fmt.Sprintf("invalid session id: %q", sessionID))
}

func kindEnum() []any {
	out := make([]any, len(notify.Kinds))
	for i, k := range notify.Kinds {
		out[i] = k
	}
	return out
}
