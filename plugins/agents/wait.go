package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	turnTopic          = "agents.turn"
	defaultWaitSeconds = 120
	maxWaitSeconds     = 300
	turnReplyLimit     = 4000
)

var errNoWaitSessions = errors.New("sessions is empty")

type waitArgs struct {
	Sessions []string `json:"sessions"`
	Timeout  int      `json:"timeout"`
}

type waitOutput struct {
	Sessions []waitResult `json:"sessions"`
	Message  *waitMessage `json:"message,omitempty"`
}

type waitMessage struct {
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments,omitempty"`
}

type waitResult struct {
	Session    string          `json:"session"`
	Status     string          `json:"status"`
	Turn       int             `json:"turn"`
	StopReason string          `json:"stopReason,omitempty"`
	Error      string          `json:"error,omitempty"`
	Asks       bool            `json:"asks"`
	Permission *waitPermission `json:"permission,omitempty"`
}

type waitPermission struct {
	Request string                 `json:"request"`
	Title   string                 `json:"title"`
	Kind    string                 `json:"kind"`
	Options []acp.PermissionOption `json:"options"`
}

type turnPayload struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Status    string `json:"status"`
	Asks      bool   `json:"asks"`
	Reply     string `json:"reply"`
}

type waiters struct {
	wakeMu sync.Mutex
	woken  chan struct{}
}

func (a *app) wait(ctx context.Context, projectID, caller string, args waitArgs) (waitOutput, error) {
	if len(args.Sessions) == 0 {
		return waitOutput{}, errNoWaitSessions
	}
	sessions := make([]*session, 0, len(args.Sessions))
	for _, id := range args.Sessions {
		s, err := a.lookup(projectID, id)
		if err != nil {
			return waitOutput{}, err
		}
		sessions = append(sessions, s)
	}
	self := a.session(caller)
	timer := time.NewTimer(waitTimeout(args.Timeout))
	defer timer.Stop()
	expired := false
	var first []waitResult
	for {
		woken := a.statusWake()
		out := make([]waitResult, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, s.waitResult())
		}
		if first == nil {
			first = out
		}
		msg := self.takeUserPrompts()
		if expired || moved(first, out) || msg != nil {
			return waitOutput{Sessions: out, Message: msg}, nil
		}
		select {
		case <-woken:
		case <-timer.C:
			expired = true
		case <-ctx.Done():
			return waitOutput{}, ctx.Err()
		}
	}
}

func moved(first, now []waitResult) bool {
	for i, r := range now {
		if r.Status != statusRunning || r.Turn != first[i].Turn {
			return true
		}
	}
	return false
}

func waitTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultWaitSeconds
	}
	return time.Duration(min(seconds, maxWaitSeconds)) * time.Second
}

func (w *waiters) statusWake() <-chan struct{} {
	w.wakeMu.Lock()
	defer w.wakeMu.Unlock()
	if w.woken == nil {
		w.woken = make(chan struct{})
	}
	return w.woken
}

func (w *waiters) wakeWaiters() {
	w.wakeMu.Lock()
	defer w.wakeMu.Unlock()
	if w.woken != nil {
		close(w.woken)
		w.woken = nil
	}
}

func (s *session) takeUserPrompts() *waitMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	var texts []string
	var attachments []attachment
	taken := false
	s.queue = slices.DeleteFunc(s.queue, func(q queuedPrompt) bool {
		if q.From != "" || slices.ContainsFunc(q.Attachments, func(a attachment) bool { return a.Kind == "image" }) {
			return false
		}
		if strings.TrimSpace(q.Text) != "" {
			texts = append(texts, q.Text)
		}
		attachments = append(attachments, q.Attachments...)
		taken = true
		return true
	})
	s.mu.Unlock()
	if !taken {
		return nil
	}
	s.changed(changeUpdate, false)
	return &waitMessage{Text: strings.Join(texts, "\n\n"), Attachments: attachments}
}

func (s *session) asks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asksLocked()
}

func (s *session) asksLocked() bool {
	return !s.running && s.rec.Status == statusDone && s.rec.Asks
}

func (s *session) waitResult() waitResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	r := waitResult{Session: s.rec.ID, Status: s.rec.Status, Turn: s.turn, Error: s.rec.Error, Asks: s.asksLocked(), Permission: s.permissionLocked()}
	if i := slices.IndexFunc(s.turns, func(t turnInfo) bool { return t.Turn == s.turn }); i >= 0 {
		r.StopReason = s.turns[i].StopReason
	}
	return r
}

func (s *session) permissionLocked() *waitPermission {
	for _, en := range s.entries {
		if en.Kind != entryPermission || s.perms[en.ID] == nil {
			continue
		}
		var call toolFields
		_ = json.Unmarshal(en.ToolCall, &call)
		return &waitPermission{Request: en.ID, Title: call.Title, Kind: call.Kind, Options: slices.Clone(en.Options)}
	}
	return nil
}

func (s *session) turnLocked() turnPayload {
	return turnPayload{Workspace: s.rec.Workspace, Session: s.rec.ID, Status: s.rec.Status, Asks: s.asksLocked(), Reply: s.replyLocked()}
}

func (s *session) replyLocked() string {
	for i := len(s.entries) - 1; i >= 0; i-- {
		en := s.entries[i]
		if en.Turn != s.turn {
			break
		}
		if en.Kind == entryAgent {
			return clipRunes(strings.TrimSpace(en.Text), turnReplyLimit)
		}
	}
	return ""
}
