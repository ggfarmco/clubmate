package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
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

	settingsSlot = "agents.settings"
	finishHint   = "Default for new sessions; change it per session in the composer options. Merge runs the tests, merges into main and removes the worktree."
)

var afkValues = map[string]bool{afkOn: true, "true": true, afkOff: false, "false": false}

var (
	errNotLive      = errors.New("the session's agent is not running")
	errUnknownMode  = errors.New("unknown mode")
	errUnknownValue = errors.New("unknown config value")
)

func (a *app) setMode(ctx context.Context, workspaceID string, args modeArgs) error {
	s, err := a.lookup(workspaceID, args.Session)
	if err != nil {
		return err
	}
	if s.snapshot().Night != "" {
		return errNightMode
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

func (a *app) setConfig(ctx context.Context, workspaceID string, args configArgs) error {
	s, err := a.lookup(workspaceID, args.Session)
	if err != nil {
		return err
	}
	if s.snapshot().Night != "" {
		return errNightConfig
	}
	switch args.ID {
	case advisorID:
		return a.setAdvisor(ctx, s, args.Value)
	case finishID:
		return a.setFinish(s, args.Value)
	case afkID:
		on, known := afkValues[args.Value]
		if !known {
			return fmt.Errorf("%w: %s=%s", errUnknownValue, afkID, args.Value)
		}
		a.setAFK(ctx, s, on, "")
		return nil
	}
	s.mu.Lock()
	i := slices.IndexFunc(s.configs, func(o acp.SessionConfigOption) bool { return o.ID == args.ID && pickable(o) })
	var option acp.SessionConfigOption
	if i >= 0 {
		option = s.configs[i]
	}
	worktree := s.rec.Worktree
	s.mu.Unlock()
	if i < 0 || !option.Valid(args.Value) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, args.ID, args.Value)
	}
	if _, err := a.sendConfig(ctx, s, option, args.Value); err != nil {
		return err
	}
	return a.host.StorageSet(ctx, configStorageKey(args.ID, worktree), args.Value)
}

func (a *app) sendConfig(ctx context.Context, s *session, option acp.SessionConfigOption, value string) ([]acp.SessionConfigOption, error) {
	p, agentID, err := a.liveProc(s)
	if err != nil {
		return nil, err
	}
	var res acp.SetSessionConfigOptionResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSetConfig, option.Request(agentID, value), &res); err != nil {
		return nil, fmt.Errorf("session/set_config_option: %w", err)
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindSetConfig, configPayload{ConfigID: option.ID, Value: value, ConfigOptions: res.ConfigOptions})
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return res.ConfigOptions, nil
}

func pickable(o acp.SessionConfigOption) bool {
	return (o.Type == acp.ConfigSelect || o.Type == acp.ConfigBoolean) && o.Category != acp.CategoryMode
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

func (a *app) setFinishDefault(ctx context.Context, value string) error {
	if !validFinish(value) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, value)
	}
	if err := a.host.StorageSet(ctx, finishKey, value); err != nil {
		return err
	}
	a.refreshSettings(ctx)
	return nil
}

func (a *app) refreshSettings(ctx context.Context) {
	if err := a.renderSettings(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render settings:", err)
	}
}

func (a *app) renderSettings(ctx context.Context) error {
	options := make([]sdui.Option, 0, len(finishChoices))
	for _, c := range finishChoices {
		options = append(options, sdui.Option{Value: c.value, Label: c.label})
	}
	rows := []sdui.Node{
		sdui.SettingRow("When a task is done", finishHint, sdui.WithAction(sdui.Select(finishID, options, a.finishDefault(ctx)), finishID)),
		sdui.SettingRow("Self-improvement", selfHint, sdui.WithAction(sdui.Toggle(selfEnabledID, a.selfEnabled(ctx)), selfEnabledID)),
	}
	if a.self.prompt() != "" || len(a.self.skills()) > 0 {
		rows = append(rows, sdui.SettingRow("The agent's prompt and skills", "", sdui.WithVariant(sdui.WithConfirm(sdui.Button(selfResetID, "Reset", ""), selfResetConfirm), sdui.VariantDanger)))
	}
	_, err := a.host.Render(ctx, "", settingsSlot, sdui.VStack(rows...))
	return err
}

func (a *app) settingsEvent(ctx context.Context, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case finishID:
		var value string
		if err := json.Unmarshal(ev.Value, &value); err != nil {
			return fmt.Errorf("%s: %w", finishID, err)
		}
		return a.setFinishDefault(ctx, value)
	case selfEnabledID:
		var on bool
		if err := json.Unmarshal(ev.Value, &on); err != nil {
			return fmt.Errorf("%s: %w", selfEnabledID, err)
		}
		return a.setSelfEnabled(ctx, on)
	case selfResetID:
		return a.resetSelf(ctx)
	}
	return fmt.Errorf("unknown action %q", ev.Action)
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

func (a *app) sessionDefaults(ctx context.Context, owner string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return a.finishDefault(ctx), owner == "" && a.afkDefault(ctx)
}

func (a *app) afkDefault(ctx context.Context) bool {
	var on bool
	if found, err := a.host.StorageGet(ctx, afkKey, &on); err != nil || !found {
		return false
	}
	return on
}

func (a *app) setAFK(ctx context.Context, s *session, on bool, by string) {
	var tell []*session
	if on {
		tell = slices.DeleteFunc(a.subtree(s), func(t *session) bool { return a.effectiveAFK(t.snapshot()) })
	}
	s.mu.Lock()
	switch {
	case !on:
		s.rec.AFKBy = ""
	case by == "" || !s.rec.AFK:
		s.rec.AFKBy = by
	}
	s.rec.AFK = on
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	for _, t := range tell {
		go a.steerText(t, afkText)
	}
	if on && by == "" {
		a.connectWaitingAway(ctx, s)
	}
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
