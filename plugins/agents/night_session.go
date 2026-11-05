package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	nightSleep   = "sleep"
	nightImprove = "improve"

	nightMode     = "default"
	nightModel    = "sonnet"
	nightMaxTurns = 60
	nightDeclare  = "mcp__" + mcpName + "__agents_self_declare"
	nightTestEnv  = "CLUBMATE_AGENTS_NIGHT_TEST"
	nightTestText = "Night test"
	rejectOnce    = "reject_once"
)

var (
	nightTools = []string{"Read", "Grep", "Glob", "Edit", "Write"}

	errNightSpec    = errors.New("a night session needs a kind, a system prompt and absolute paths")
	errNightTest    = errors.New("agents.night-spawn runs only in tests")
	errNightMode    = errors.New("a night session runs only in default mode")
	errNightConfig  = errors.New("a night session's settings don't change")
	errNightHandoff = errors.New("a night session can't be handed off")
	errNightBacklog = errors.New("a night session's items don't go to the backlog")
)

type nightSpec struct {
	Kind, Workspace, Dir, Title, Prompt, System string
	Reads                                       []string
}

type nightSpawnArgs struct {
	Dir  string `json:"dir"`
	Kind string `json:"kind"`
}

func (a *app) spawnNight(spec nightSpec) (*session, error) {
	if !spec.confinable() {
		return nil, errNightSpec
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if _, _, err := workspaceRoot(ctx, a.host, spec.Workspace); err != nil {
		return nil, err
	}
	rec := record{Workspace: spec.Workspace, Agent: agentClaude, Worktree: spec.Dir, Title: titleOf(spec.Title), TitleSet: spec.Title != "", Night: spec.Kind, Reads: spec.Reads, Allow: nightAllow(spec.Dir, spec.Reads), SystemPrompt: spec.System, StartMode: nightMode, StartModel: nightModel, Notify: notifyMuted, AFK: true}
	return a.spawn(rec, promptArgs{Text: spec.Prompt}, nil)
}

func (s nightSpec) confinable() bool {
	if s.Kind != nightSleep && s.Kind != nightImprove || s.System == "" {
		return false
	}
	return !slices.ContainsFunc(append([]string{s.Dir}, s.Reads...), func(dir string) bool { return !filepath.IsAbs(dir) })
}

func nightAllow(dir string, reads []string) []string {
	out := []string{"Edit(/" + dir + "/**)"}
	for _, r := range reads {
		out = append(out, "Read(/"+r+"/**)")
	}
	return append(out, nightDeclare)
}

func (s *session) noteRefused() {
	s.mu.Lock()
	s.rec.Refused++
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func nightOutcome(options []acp.PermissionOption) acp.PermissionOutcome {
	i := slices.IndexFunc(options, func(o acp.PermissionOption) bool { return o.Kind == rejectOnce })
	if i < 0 {
		return acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
	}
	return acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: options[i].OptionID}
}

func hiddenFrom(cmd pluginsdk.CommandRun, rec record) bool {
	return rec.Night != "" && cmd.Agent && cmd.Caller != rec.ID
}

func visibleInfos(cmd pluginsdk.CommandRun, infos []sessionInfo) []sessionInfo {
	return slices.DeleteFunc(infos, func(info sessionInfo) bool { return hiddenFrom(cmd, info.record) })
}

func (a *app) lookupFor(cmd pluginsdk.CommandRun, id string) (*session, error) {
	s, err := a.lookup(cmd.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if hiddenFrom(cmd, s.snapshot()) {
		return nil, fmt.Errorf("%w: %s", errNoSession, id)
	}
	return s, nil
}

func (a *app) nightSpawnCommand(_ context.Context, cmd pluginsdk.CommandRun, args nightSpawnArgs) (any, error) {
	if os.Getenv(nightTestEnv) != "1" {
		return nil, errNightTest
	}
	s, err := a.spawnNight(nightSpec{Kind: args.Kind, Workspace: cmd.WorkspaceID, Dir: args.Dir, Title: nightTestText, Prompt: nightTestText, System: nightTestText})
	if err != nil {
		return nil, err
	}
	return newResult{Session: s.snapshot().ID, Worktree: args.Dir}, nil
}
