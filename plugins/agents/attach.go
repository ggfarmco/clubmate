package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"clubmate/plugins/agents/acp"
)

var (
	errNoResume = errors.New("the agent cannot resume this session")
)

func (a *app) attachInBackground(s *session) {
	if _, err := a.attach(s); err != nil {
		a.fail(s, err.Error())
	}
}

func (a *app) fail(s *session, text string) {
	s.mu.Lock()
	if s.rec.Closed {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.recordLocked(actorClubmate, kindError, turnEndPayload{Error: text})
	s.setStatusLocked(statusError, text)
	ended := s.turnLocked()
	s.mu.Unlock()
	s.turned(ended)
	s.changed(changeStatus, true)
}

func (a *app) resume(s *session) {
	s.mu.Lock()
	if s.rec.Closed {
		s.rec.Closed = false
		s.setStatusLocked(statusIdle, "")
	}
	id := s.rec.ID
	s.mu.Unlock()
	a.mu.Lock()
	if a.dropped[id] == dropClosed {
		delete(a.dropped, id)
	}
	a.mu.Unlock()
	s.changed(changeStatus, true)
	go a.attachInBackground(s)
}

func (a *app) attach(s *session) (*agentProc, error) {
	if err := a.worktreeReady(s).wait(); err != nil {
		return nil, err
	}
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	s.loadLocked()
	p, rec, prompted := s.proc, s.rec, s.prompted
	s.mu.Unlock()
	if p != nil && p.alive() {
		return p, nil
	}
	if rec.Closed {
		return nil, errClosed
	}
	p, err := a.ensureProc(context.Background(), rec.Agent, rec.Workspace, rec.Worktree)
	a.setNodeProblem(err)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()
	var extra []string
	if p.caps.SessionCapabilities.AdditionalDirectories != nil {
		if project, err := a.host.GetProject(ctx, rec.Workspace); err == nil {
			_, extra, _ = workRoot(project)
		}
	}
	if rec.AgentSessionID != "" && prompted {
		return p, a.resumeOn(ctx, s, p, rec, extra)
	}
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionNew, acp.NewSessionRequest{Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcp, Meta: sessionMeta(rec.ID, a.sessionAdvisor(ctx, rec))}, &raw); err != nil {
		a.release(p)
		if authErr := a.authRequired(rec.ID, p, err); authErr != nil {
			return nil, authErr
		}
		return nil, fmt.Errorf("session/new: %w", err)
	}
	var res acp.NewSessionResponse
	if err := json.Unmarshal(raw, &res); err != nil || res.SessionID == "" {
		a.release(p)
		return nil, fmt.Errorf("session/new: bad response %s", raw)
	}
	s.mu.Lock()
	s.rec.AgentSessionID = res.SessionID
	if s.rec.Status == statusError {
		s.setStatusLocked(statusIdle, "")
	}
	s.mu.Unlock()
	a.notes.persist(ctx, s)
	p.register(res.SessionID, s)
	s.mu.Lock()
	s.recordLocked(s.actor(), kindNew, raw)
	s.mu.Unlock()
	s.changed(changeStatus, true)
	mode, value := a.startSettings(ctx, rec)
	a.applyMode(ctx, s, res.Modes, mode)
	a.applyConfigs(ctx, s, res.ConfigOptions, value)
	return p, nil
}

func (a *app) worktreeReady(s *session) *setup {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setup.failed() {
		s.setup = newSetup(s.setup.args)
		go a.createWorktree(s, s.setup)
	}
	return s.setup
}

func (a *app) resumeOn(ctx context.Context, s *session, p *agentProc, rec record, extra []string) error {
	if p.caps.SessionCapabilities.Resume == nil {
		a.release(p)
		a.setNoResume(rec.ID, true)
		return errNoResume
	}
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionResume, acp.ResumeSessionRequest{SessionID: rec.AgentSessionID, Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcp, Meta: sessionMeta(rec.ID, a.sessionAdvisor(ctx, rec))}, &raw); err != nil {
		a.release(p)
		if authErr := a.authRequired(rec.ID, p, err); authErr != nil {
			return authErr
		}
		a.setNoResume(rec.ID, true)
		return fmt.Errorf("session/resume: %w", err)
	}
	a.setNoResume(rec.ID, false)
	p.register(rec.AgentSessionID, s)
	s.mu.Lock()
	s.recordLocked(s.actor(), kindResume, raw)
	if s.rec.Status == statusError {
		s.setStatusLocked(statusIdle, "")
	}
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return nil
}

func (a *app) startSettings(ctx context.Context, rec record) (string, func(id string) string) {
	if rec.ParentSession == "" {
		return a.stored(ctx, modeStorageKey(rec.Worktree)), func(id string) string { return a.stored(ctx, configStorageKey(id, rec.Worktree)) }
	}
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return "", func(string) string { return "" }
	}
	mode, values := parent.settings()
	return mode, func(id string) string { return values[id] }
}

func (a *app) stored(ctx context.Context, key string) string {
	var value string
	if found, err := a.host.StorageGet(ctx, key, &value); err != nil || !found {
		return ""
	}
	return value
}

func (a *app) applyMode(ctx context.Context, s *session, modes *acp.SessionModeState, mode string) {
	if modes == nil || mode == "" || mode == modes.CurrentModeID {
		return
	}
	if !slices.ContainsFunc(modes.AvailableModes, func(m acp.SessionMode) bool { return m.ID == mode }) {
		return
	}
	if err := a.sendMode(ctx, s, mode); err != nil {
		fmt.Fprintln(os.Stderr, "agents: restore mode:", err)
	}
}

func (a *app) applyConfigs(ctx context.Context, s *session, configs []acp.SessionConfigOption, valueOf func(id string) string) {
	for i := 0; i < len(configs); i++ {
		o := configs[i]
		if !pickable(o) {
			continue
		}
		value := valueOf(o.ID)
		if value == "" || value == o.Current() {
			continue
		}
		if _, ok := o.Choice(value); !ok {
			continue
		}
		next, err := a.sendConfig(ctx, s, o.ID, value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: restore config:", err)
			continue
		}
		if next != nil {
			configs = next
		}
	}
}
