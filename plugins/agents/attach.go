package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"clubmate/plugins/agents/acp"
)

var (
	errNoResume = errors.New("the agent cannot resume this session")
)

const (
	startMissing  = "%s %s is not available"
	startNoChoice = "The agent offers no %s choice; the session runs on its default %s."
	startChoices  = "The session runs on its current %s. %s offered: %s."
)

type startConfig struct {
	category string
	name     string
	noun     string
	offered  string
	value    string
}

func (a *app) attachInBackground(s *session) {
	if _, err := a.attach(s); err != nil {
		a.fail(s, err.Error())
	}
}

func (a *app) fail(s *session, text string) {
	s.mu.Lock()
	if s.rec.closed() {
		s.mu.Unlock()
		return
	}
	running := s.running
	s.running = false
	s.recordLocked(actorClubmate, kindError, turnEndPayload{Error: text})
	s.setStatusLocked(statusError, text)
	ended := s.turnLocked()
	s.mu.Unlock()
	s.turned(ended)
	s.changed(changeStatus, true)
	if running {
		a.notifyEnd(s, ended)
	}
}

func (a *app) resume(s *session) {
	s.mu.Lock()
	if s.rec.closed() {
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
	p, err := a.attachAgent(s)
	if err != nil {
		return nil, err
	}
	if rec := s.snapshot(); rec.Night != "" && rec.Mode != nightMode {
		return nil, fmt.Errorf("%w: %q", errNightMode, rec.Mode)
	}
	return p, nil
}

func (a *app) attachAgent(s *session) (*agentProc, error) {
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
	if rec.closed() {
		return nil, errClosed
	}
	p, err := a.ensureProc(context.Background(), rec.Agent, rec.Workspace, rec.Worktree)
	a.setNodeProblem(err)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()
	extra := rec.Reads
	if rec.Night == "" && rec.RepoPath == "" && p.caps.SessionCapabilities.AdditionalDirectories != nil {
		if workspace, err := a.host.GetWorkspace(ctx, rec.Workspace); err == nil {
			_, extra, _ = workRoot(workspace)
		}
	}
	if rec.AgentSessionID != "" && prompted {
		return p, a.resumeOn(ctx, s, p, rec, extra)
	}
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionNew, acp.NewSessionRequest{Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcpFor(rec), Meta: sessionMeta(rec, a.sessionAdvisor(ctx, rec), a.selfFor(ctx, rec))}, &raw); err != nil {
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
	p.register(res.SessionID, s)
	s.mu.Lock()
	s.rec.AgentSessionID = res.SessionID
	if s.rec.Status == statusError {
		s.setStatusLocked(statusIdle, "")
	}
	s.recordLocked(s.actor(), kindNew, raw)
	s.mu.Unlock()
	a.notes.persist(ctx, s)
	s.changed(changeStatus, true)
	mode, value := a.startSettings(ctx, rec)
	a.applyMode(ctx, s, res.Modes, cmp.Or(rec.StartMode, rec.FirstMode, mode))
	starts := startConfigs(rec)
	configs, failed := a.applyConfigs(ctx, s, res.ConfigOptions, withStart(starts, value))
	reportStart(s, configs, starts, failed)
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
	mode, values := s.settings()
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionResume, acp.ResumeSessionRequest{SessionID: rec.AgentSessionID, Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcpFor(rec), Meta: sessionMeta(rec, a.sessionAdvisor(ctx, rec), a.selfFor(ctx, rec))}, &raw); err != nil {
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
	if rec.Night != "" {
		if err := a.sendMode(ctx, s, nightMode); err != nil {
			a.detach(s, p, rec.AgentSessionID)
			return err
		}
	}
	var res acp.ResumeSessionResponse
	if json.Unmarshal(raw, &res) == nil {
		if rec.Night == "" {
			a.applyMode(ctx, s, res.Modes, cmp.Or(rec.StartMode, mode))
		}
		a.applyConfigs(ctx, s, res.ConfigOptions, withStart([]startConfig{{category: acp.CategoryModel, value: rec.pinnedModel()}}, func(id string) string { return values[id] }))
	}
	return nil
}

func (a *app) detach(s *session, p *agentProc, agentID string) {
	s.mu.Lock()
	s.proc = nil
	s.mu.Unlock()
	p.unregister(agentID)
	a.release(p)
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
	if parent.snapshot().StartMode != "" {
		mode = a.stored(ctx, modeStorageKey(rec.Worktree))
	}
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

func (a *app) applyConfigs(ctx context.Context, s *session, configs []acp.SessionConfigOption, valueOf func(o acp.SessionConfigOption) string) ([]acp.SessionConfigOption, []string) {
	var failed []string
	for i := 0; i < len(configs); i++ {
		o := configs[i]
		if !pickable(o) {
			continue
		}
		value := valueOf(o)
		if _, live := s.settings(); value == "" || value == live[o.ID] {
			continue
		}
		if !o.Valid(value) {
			failed = append(failed, o.ID)
			continue
		}
		next, err := a.sendConfig(ctx, s, o, value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: restore config:", err)
			failed = append(failed, o.ID)
			continue
		}
		if next != nil {
			configs = next
		}
	}
	return configs, failed
}

func startConfigs(rec record) []startConfig {
	return []startConfig{
		{category: acp.CategoryModel, name: "Model", noun: "model", offered: "Models", value: rec.StartModel},
		{category: acp.CategoryThoughtLevel, name: "Effort", noun: "effort", offered: "Effort levels", value: rec.StartEffort},
	}
}

func (c startConfig) of(o acp.SessionConfigOption) bool {
	return o.Category == c.category
}

func reportStart(s *session, configs []acp.SessionConfigOption, starts []startConfig, failed []string) {
	var notes []string
	for _, c := range starts {
		if c.value == "" {
			continue
		}
		var why string
		switch i := slices.IndexFunc(configs, c.of); {
		case i < 0:
			why = fmt.Sprintf(startNoChoice, c.noun, c.noun)
		case slices.Contains(failed, configs[i].ID):
			var offered []string
			for _, choice := range configs[i].Choices() {
				offered = append(offered, choice.Value)
			}
			why = fmt.Sprintf(startChoices, c.noun, c.offered, strings.Join(offered, ", "))
		default:
			continue
		}
		title := fmt.Sprintf(startMissing, c.name, c.value)
		s.notice(title, why)
		notes = append(notes, title+". "+why)
	}
	if len(notes) > 0 {
		s.setStartNotice(strings.Join(notes, " "))
	}
}

func withStart(starts []startConfig, valueOf func(id string) string) func(o acp.SessionConfigOption) string {
	return func(o acp.SessionConfigOption) string {
		for _, c := range starts {
			if c.value != "" && c.of(o) {
				return c.value
			}
		}
		return valueOf(o.ID)
	}
}
