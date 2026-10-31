package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"clubmate/plugins/agents/acp"
)

const (
	advisorID   = "advisor"
	finishID    = "finish"
	finishAsk   = "ask"
	finishMerge = "merge"
	afkID       = "afk"
	afkOn       = "on"
	afkOff      = "off"
)

var (
	errNotLive      = errors.New("the session's agent is not running")
	errUnknownMode  = errors.New("unknown mode")
	errUnknownValue = errors.New("unknown config value")
)

func (a *app) setMode(ctx context.Context, projectID string, args modeArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	s.mu.Lock()
	known := slices.ContainsFunc(s.modes, func(m acp.SessionMode) bool { return m.ID == args.Mode })
	worktree := s.rec.Worktree
	s.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: %s", errUnknownMode, args.Mode)
	}
	if err := a.sendMode(ctx, s, args.Mode); err != nil {
		return err
	}
	return a.host.StorageSet(ctx, modeStorageKey(worktree), args.Mode)
}

func (a *app) sendMode(ctx context.Context, s *session, mode string) error {
	p, agentID, err := a.liveProc(s)
	if err != nil {
		return err
	}
	req := acp.SetSessionModeRequest{SessionID: agentID, ModeID: mode}
	if err := p.conn.Call(ctx, acp.MethodSessionSetMode, req, nil); err != nil {
		return fmt.Errorf("session/set_mode: %w", err)
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindSetMode, req)
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return nil
}

func (a *app) setConfig(ctx context.Context, projectID string, args configArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	switch args.ID {
	case advisorID:
		return a.setAdvisor(ctx, s, args.Value)
	case finishID:
		return a.setFinish(s, args.Value)
	case afkID:
		if args.Value != afkOn && args.Value != afkOff {
			return fmt.Errorf("%w: %s=%s", errUnknownValue, afkID, args.Value)
		}
		a.setAFK(s, args.Value == afkOn)
		return nil
	}
	s.mu.Lock()
	i := slices.IndexFunc(s.configs, func(o acp.SessionConfigOption) bool { return o.ID == args.ID && pickable(o) })
	var known bool
	if i >= 0 {
		_, known = s.configs[i].Choice(args.Value)
	}
	worktree := s.rec.Worktree
	s.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, args.ID, args.Value)
	}
	if _, err := a.sendConfig(ctx, s, args.ID, args.Value); err != nil {
		return err
	}
	return a.host.StorageSet(ctx, configStorageKey(args.ID, worktree), args.Value)
}

func (a *app) sendConfig(ctx context.Context, s *session, id, value string) ([]acp.SessionConfigOption, error) {
	p, agentID, err := a.liveProc(s)
	if err != nil {
		return nil, err
	}
	var res acp.SetSessionConfigOptionResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSetConfig, acp.SetSessionConfigOptionRequest{SessionID: agentID, ConfigID: id, Value: value}, &res); err != nil {
		return nil, fmt.Errorf("session/set_config_option: %w", err)
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindSetConfig, configPayload{ConfigID: id, Value: value, ConfigOptions: res.ConfigOptions})
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return res.ConfigOptions, nil
}

func pickable(o acp.SessionConfigOption) bool {
	return o.Type == acp.ConfigSelect && o.Category != acp.CategoryMode
}

func (a *app) setAdvisor(ctx context.Context, s *session, value string) error {
	if !slices.ContainsFunc(advisorChoices, func(c advisorChoice) bool { return c.value == value }) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, advisorID, value)
	}
	s.mu.Lock()
	child, worktree := s.rec.ParentSession != "", s.rec.Worktree
	if child {
		s.rec.Advisor = value
	}
	s.mu.Unlock()
	if child {
		s.changed(changeUpdate, true)
		return nil
	}
	if err := a.host.StorageSet(ctx, advisorStorageKey(worktree), value); err != nil {
		return err
	}
	a.uiMu.Lock()
	a.advisors[worktree] = value
	a.uiMu.Unlock()
	s.changed(changeUpdate, false)
	return nil
}

func (a *app) sessionAdvisor(ctx context.Context, rec record) string {
	if rec.ParentSession != "" {
		return rec.Advisor
	}
	return a.advisor(ctx, rec.Worktree)
}

func (a *app) advisor(ctx context.Context, worktree string) string {
	a.uiMu.Lock()
	value, ok := a.advisors[worktree]
	a.uiMu.Unlock()
	if ok {
		return value
	}
	if _, err := a.host.StorageGet(ctx, advisorStorageKey(worktree), &value); err != nil {
		return ""
	}
	a.uiMu.Lock()
	a.advisors[worktree] = value
	a.uiMu.Unlock()
	return value
}

func (a *app) finishDefault(ctx context.Context) string {
	var value string
	if found, err := a.host.StorageGet(ctx, finishKey, &value); err != nil || !found || !validFinish(value) {
		return finishAsk
	}
	return value
}

func validFinish(value string) bool {
	return slices.ContainsFunc(finishChoices, func(c advisorChoice) bool { return c.value == value })
}

func (a *app) setFinish(s *session, value string) error {
	if !validFinish(value) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, value)
	}
	s.mu.Lock()
	s.rec.Finish = value
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return nil
}

func (a *app) sessionDefaults(ctx context.Context, parent string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return a.finishDefault(ctx), parent == "" && a.afkDefault(ctx)
}

func (a *app) afkDefault(ctx context.Context) bool {
	var on bool
	if found, err := a.host.StorageGet(ctx, afkKey, &on); err != nil || !found {
		return false
	}
	return on
}

func (a *app) setAFK(s *session, on bool) {
	s.mu.Lock()
	s.rec.AFK = on
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (a *app) effectiveAFK(rec record) bool {
	for r := range a.ancestry(rec) {
		if r.AFK {
			return true
		}
	}
	return false
}

func (a *app) setNotify(s *session, policy string) {
	s.mu.Lock()
	s.rec.Notify = storedNotify(policy)
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (a *app) effectiveNotify(rec record) string {
	rank := 0
	for r := range a.ancestry(rec) {
		rank = max(rank, slices.Index(notifyPolicies, cmp.Or(r.Notify, notifyAll)))
	}
	return notifyPolicies[rank]
}
