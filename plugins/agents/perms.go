package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
)

var (
	errUnknownRequest = errors.New("unknown permission request")
	errUnknownOption  = errors.New("unknown permission option")
)

type pendingPermission struct {
	request string
	at      time.Time
	reply   chan acp.PermissionOutcome
	elicit  chan acp.ElicitationResponse
}

func (s *session) requestPermission(ctx context.Context, request string, params json.RawMessage) acp.PermissionOutcome {
	perm := &pendingPermission{request: request, at: time.Now().UTC(), reply: make(chan acp.PermissionOutcome, 1)}
	s.mu.Lock()
	s.loadLocked()
	id := entryPermission + "-" + strconv.Itoa(s.nextID+1)
	s.perms[id] = perm
	s.recordLocked(s.actor(), kindPermission, permissionPayload{ID: id, RequestID: request, Params: params})
	s.settleLocked()
	waiting := s.turnLocked()
	s.mu.Unlock()
	s.turned(waiting)
	s.changed(changeStatus, true)
	if s.requested != nil {
		s.requested(entryPermission, id)
	}
	var out acp.PermissionOutcome
	select {
	case out = <-perm.reply:
	case <-ctx.Done():
		out = acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
	}
	s.mu.Lock()
	delete(s.perms, id)
	s.recordLocked(actorUser, kindAnswer, answerPayload{Request: id, Outcome: out})
	s.settleLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	if s.resolved != nil {
		s.resolved(entryPermission, id)
	}
	return out
}

func (s *session) answer(id, option string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	perm, ok := s.perms[id]
	if !ok || perm.elicit != nil {
		return errUnknownRequest
	}
	en := s.entries[s.index[key(entryPermission, id)]]
	if !slices.ContainsFunc(en.Options, func(o acp.PermissionOption) bool { return o.OptionID == option }) {
		return errUnknownOption
	}
	delete(s.perms, id)
	perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: option}
	return nil
}

func (s *session) pending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := slices.Collect(maps.Keys(s.perms))
	slices.Sort(ids)
	return ids
}

func (s *session) onlyQuestions() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.perms) == 0 {
		return false
	}
	for id, perm := range s.perms {
		i, ok := s.index[key(entryElicitation, id)]
		if perm.elicit == nil || !ok || s.entries[i].Elicitation == nil || s.entries[i].Elicitation.Mode != acp.ElicitationForm {
			return false
		}
	}
	return true
}

func (s *session) permissionSummary() *pendingSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, en := range s.entries {
		if summary := s.summaryLocked(en); summary != nil {
			return summary
		}
	}
	return nil
}

func (s *session) requestSummary(kind, id string) *pendingSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.index[key(kind, id)]
	if !ok {
		return nil
	}
	return s.summaryLocked(s.entries[i])
}

func (s *session) summaryLocked(en *entry) *pendingSummary {
	perm := s.perms[en.ID]
	if perm == nil {
		return nil
	}
	switch {
	case en.Kind == entryElicitation && en.Elicitation != nil:
		return &pendingSummary{Kind: pendingQuestion, Text: clipRunes(firstLine(en.Elicitation.Message), summaryRunes), At: perm.at}
	case en.Kind == entryPermission:
		var call toolFields
		_ = json.Unmarshal(en.ToolCall, &call)
		text := call.Title
		if command, ok := call.RawInput["command"].(string); ok && strings.TrimSpace(command) != "" {
			text = command
		}
		return &pendingSummary{Kind: entryPermission, Text: clipRunes(firstLine(text), summaryRunes), Tool: call.Kind, At: perm.at}
	}
	return nil
}

func (s *session) pendingIDs() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.perms))
	for id := range s.perms {
		out[id] = true
	}
	return out
}

func (s *session) cancelRequest(request string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, perm := range s.perms {
		if perm.request == request {
			delete(s.perms, id)
			perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
			return true
		}
	}
	return false
}

func (s *session) cancelPermissionsLocked() {
	for id, perm := range s.perms {
		delete(s.perms, id)
		perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
	}
}
