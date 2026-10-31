package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"

	"clubmate/plugins/agents/acp"
)

var (
	errUnknownRequest = errors.New("unknown permission request")
	errUnknownOption  = errors.New("unknown permission option")
)

type pendingPermission struct {
	request string
	reply   chan acp.PermissionOutcome
}

func (s *session) requestPermission(ctx context.Context, request string, params json.RawMessage) acp.PermissionOutcome {
	perm := &pendingPermission{request: request, reply: make(chan acp.PermissionOutcome, 1)}
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
	return out
}

func (s *session) answer(id, option string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	perm, ok := s.perms[id]
	if !ok {
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
