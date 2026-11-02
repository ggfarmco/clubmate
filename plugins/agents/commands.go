package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	remotePlugin  = "remote"
	voicePlugin   = "voice"
	cronPlugin    = "cron"
	triggerPlugin = "triggers"
	unknownCaller = "unknown"
)

var (
	commandTable = map[string]commandFunc{
		"agents.new":            withArgs((*app).newCommand),
		"agents.prompt":         withArgs((*app).promptCommand),
		"agents.answer":         withArgs((*app).answerCommand),
		"agents.permission":     withArgs((*app).permissionCommand),
		"agents.elicitation":    withArgs((*app).elicitationCommand),
		"agents.mode":           withArgs((*app).modeCommand),
		"agents.config":         withArgs((*app).configCommand),
		"agents.sessions":       withArgs((*app).sessionsCommand),
		"agents.search":         withArgs((*app).searchCommand),
		"agents.notify":         withArgs((*app).notifyCommand),
		"agents.finish-default": withArgs((*app).finishDefaultCommand),
		"agents.afk":            withArgs((*app).afkCommand),
		"agents.afk-default":    withArgs((*app).afkDefaultCommand),
		"agents.usage":          (*app).usageCommand,
		"agents.stop-root":      withArgs((*app).stopRootCommand),
		"agents.wait":           withArgs((*app).waitCommand),
		"agents.transcript":     withArgs((*app).transcriptCommand),
		"agents.blame":          typed((*app).blameCommand),
		"agents.trace":          typed((*app).traceCommand),
		"agents.docs":           withArgs((*app).docsCommand),
		"agents.image":          withArgs((*app).imageCommand),
		"agents.export":         withArgs((*app).exportCommand),
		"agents.import":         withArgs((*app).importCommand),
		"agents.handoff":        withArgs((*app).handoffCommand),
		"agents.cancel":         withSession((*app).cancelCommand),
		"agents.stop-subagent":  withArgs((*app).stopSubagentCommand),
		"agents.close":          withSession((*app).closeCommand),
		"agents.delete":         withSession((*app).deleteCommand),
		"agents.resume":         withSession((*app).resumeCommand),
		"agents.reveal":         withSession((*app).revealCommand),
		"agents.reviewed":       withSession((*app).reviewedCommand),
	}

	errUnknownCmd    = errors.New("unknown command")
	errRelativeRoot  = errors.New("stop-root: path must be absolute")
	errNotDescendant = errors.New("the session was not started by this agent")
	errVoiceOnly     = errors.New("invalid_params: only the voice plugin may set disallowedTools, systemPrompt, mode or model")
	errOriginOnly    = errors.New("invalid_params: only the cron plugin may set cronJob and only the triggers plugin may set trigger")
	errFinishUser    = errors.New("invalid_params: only the user may set finish")
)

type commandFunc func(a *app, ctx context.Context, cmd pluginsdk.CommandRun) (any, error)

type sessionArgs struct {
	Session string `json:"session"`
}

type sessionsArgs struct {
	Session string  `json:"session"`
	Since   *string `json:"since"`
}

type subagentArgs struct {
	Session  string `json:"session"`
	Subagent string `json:"subagent"`
}

type newArgs struct {
	Agent           string         `json:"agent"`
	Branch          string         `json:"branch"`
	Prompt          string         `json:"prompt"`
	Title           string         `json:"title"`
	Notify          string         `json:"notify"`
	Run             string         `json:"run"`
	RunTitle        string         `json:"runTitle"`
	RunAfter        []api.RunAfter `json:"runAfter"`
	DisallowedTools []string       `json:"disallowedTools"`
	SystemPrompt    string         `json:"systemPrompt"`
	Mode            string         `json:"mode"`
	Model           string         `json:"model"`
	AFK             *bool          `json:"afk"`
	Finish          string         `json:"finish"`
	CronJob         string         `json:"cronJob"`
	Trigger         string         `json:"trigger"`
}

type newResult struct {
	Session  string `json:"session"`
	Worktree string `json:"worktree"`
	parent   string
}

type promptArgs struct {
	Session     string       `json:"session"`
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments"`
	From        string       `json:"-"`
	Voice       bool         `json:"-"`
}

type attachment struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
	MimeType  string `json:"mimeType,omitempty"`
	Data      string `json:"data,omitempty"`
}

type answerArgs struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

type permissionArgs struct {
	Session string `json:"session"`
	Request string `json:"request"`
	Option  string `json:"option"`
}

type notifyArgs struct {
	Session string `json:"session"`
	Policy  string `json:"policy"`
}

type modeArgs struct {
	Session string `json:"session"`
	Mode    string `json:"mode"`
}

type configArgs struct {
	Session string `json:"session"`
	ID      string `json:"id"`
	Value   string `json:"value"`
}

type afkArgs struct {
	Session string `json:"session"`
	On      bool   `json:"on"`
}

type afkDefaultArgs struct {
	On *bool `json:"on"`
}

type afkResult struct {
	On bool `json:"on"`
}

type finishArgs struct {
	Value string `json:"value"`
}

type finishResult struct {
	Value string `json:"value"`
}

type pathArgs struct {
	Path string `json:"path"`
}

type transcriptArgs struct {
	Session  string `json:"session"`
	FromTurn int    `json:"fromTurn"`
	Compact  bool   `json:"compact"`
	Last     bool   `json:"last"`
	Since    string `json:"since"`
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.loaded:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	run, ok := commandTable[cmd.ID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownCmd, cmd.ID)
	}
	return run(a, ctx, cmd)
}

func withArgs[T any](fn func(*app, context.Context, pluginsdk.CommandRun, T) (any, error)) commandFunc {
	return func(a *app, ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
		var args T
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return fn(a, ctx, cmd, args)
	}
}

func typed[T any](fn func(*app, context.Context, pluginsdk.CommandRun) (T, error)) commandFunc {
	return func(a *app, ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
		return fn(a, ctx, cmd)
	}
}

func withSession(fn func(*app, context.Context, pluginsdk.CommandRun, *session) (any, error)) commandFunc {
	return withArgs(func(a *app, ctx context.Context, cmd pluginsdk.CommandRun, args sessionArgs) (any, error) {
		s, err := a.lookup(cmd.WorkspaceID, args.Session)
		if err != nil {
			return nil, err
		}
		return fn(a, ctx, cmd, s)
	})
}

func (a *app) newCommand(ctx context.Context, cmd pluginsdk.CommandRun, args newArgs) (any, error) {
	restricted := args.restricted()
	if restricted && (cmd.Agent || cmd.Via != voicePlugin) {
		return nil, errVoiceOnly
	}
	if args.CronJob != "" && cmd.Via != cronPlugin || args.Trigger != "" && cmd.Via != triggerPlugin {
		return nil, errOriginOnly
	}
	if args.Finish != "" && cmd.Agent {
		return nil, errFinishUser
	}
	res, err := a.newSession(ctx, a.host, cmd.WorkspaceID, origin(cmd), args)
	if err != nil {
		return nil, err
	}
	if res.parent == "" && !restricted {
		if err := a.selectSession(ctx, cmd.WorkspaceID, res.Session); err != nil {
			return nil, err
		}
	}
	a.kick(cmd.WorkspaceID)
	return res, nil
}

func (a *app) promptCommand(_ context.Context, cmd pluginsdk.CommandRun, args promptArgs) (any, error) {
	args.From = origin(cmd)
	args.Voice = !cmd.Agent && cmd.Via == voicePlugin
	return nil, a.prompt(cmd.WorkspaceID, args)
}

func (a *app) answerCommand(_ context.Context, cmd pluginsdk.CommandRun, args answerArgs) (any, error) {
	return nil, a.prompt(cmd.WorkspaceID, promptArgs{Session: args.Session, Text: args.Text})
}

func (a *app) permissionCommand(_ context.Context, cmd pluginsdk.CommandRun, args permissionArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if !asUser(cmd) && (cmd.Caller == "" || !a.descendantOf(args.Session, cmd.Caller)) {
		return nil, errNotDescendant
	}
	return nil, s.answer(args.Request, args.Option)
}

func (a *app) modeCommand(ctx context.Context, cmd pluginsdk.CommandRun, args modeArgs) (any, error) {
	return nil, a.setMode(ctx, cmd.WorkspaceID, args)
}

func (a *app) configCommand(ctx context.Context, cmd pluginsdk.CommandRun, args configArgs) (any, error) {
	return nil, a.setConfig(ctx, cmd.WorkspaceID, args)
}

func (a *app) sessionsCommand(_ context.Context, cmd pluginsdk.CommandRun, args sessionsArgs) (any, error) {
	infos := a.infos(cmd.WorkspaceID)
	if args.Since == nil {
		return onlySession(infos, args.Session), nil
	}
	page := a.sessionRevs.page(cmd.WorkspaceID, infos, *args.Since)
	page.Sessions = onlySession(page.Sessions, args.Session)
	if args.Session != "" {
		page.Removed = slices.DeleteFunc(page.Removed, func(id string) bool { return id != args.Session })
	}
	return page, nil
}

func onlySession(infos []sessionInfo, id string) []sessionInfo {
	if id == "" {
		return infos
	}
	return slices.DeleteFunc(infos, func(info sessionInfo) bool { return info.ID != id })
}

func (a *app) notifyCommand(_ context.Context, cmd pluginsdk.CommandRun, args notifyArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if cmd.Agent && args.Session != cmd.Caller && !a.descendantOf(args.Session, cmd.Caller) {
		return nil, errNotDescendant
	}
	a.setNotify(s, args.Policy)
	return nil, nil
}

func (a *app) finishDefaultCommand(ctx context.Context, _ pluginsdk.CommandRun, args finishArgs) (any, error) {
	if args.Value == "" {
		return finishResult{Value: a.finishDefault(ctx)}, nil
	}
	if err := a.setFinishDefault(ctx, args.Value); err != nil {
		return finishResult{}, err
	}
	return finishResult(args), nil
}

func (a *app) afkCommand(_ context.Context, cmd pluginsdk.CommandRun, args afkArgs) (any, error) {
	args.Session = cmp.Or(args.Session, cmd.Caller)
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if cmd.Agent && args.Session != cmd.Caller && !a.descendantOf(args.Session, cmd.Caller) {
		return nil, errNotDescendant
	}
	a.setAFK(s, args.On)
	return nil, nil
}

func (a *app) afkDefaultCommand(ctx context.Context, _ pluginsdk.CommandRun, args afkDefaultArgs) (any, error) {
	if args.On == nil {
		return afkResult{On: a.afkDefault(ctx)}, nil
	}
	return afkResult{On: *args.On}, a.host.StorageSet(ctx, afkKey, *args.On)
}

func (a *app) stopRootCommand(ctx context.Context, _ pluginsdk.CommandRun, args pathArgs) (any, error) {
	if !filepath.IsAbs(args.Path) {
		return nil, errRelativeRoot
	}
	return nil, a.stopRoot(ctx, filepath.Clean(args.Path))
}

func (a *app) waitCommand(ctx context.Context, cmd pluginsdk.CommandRun, args waitArgs) (any, error) {
	return a.wait(ctx, cmd.WorkspaceID, cmd.Caller, args)
}

func (a *app) transcriptCommand(ctx context.Context, cmd pluginsdk.CommandRun, args transcriptArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	since, delta := parseCursor(args.Since)
	if !delta || args.Last {
		since, delta = -1, false
	}
	v := s.viewSince(args.FromTurn, since)
	v.Delta = delta
	if !cmd.Agent {
		name := ""
		if w, err := a.host.GetWorkspace(ctx, cmd.WorkspaceID); err == nil {
			name = w.Name
		}
		withPermissionContent(v.Session, v.Entries, name)
	}
	if args.Last {
		v = v.lastTurn()
	} else if v.Session.Agent == agentClaude {
		v.Advisor = a.sessionAdvisor(ctx, v.Session)
	}
	if cmd.Agent || args.Compact {
		dropImageData(v.Entries)
	}
	if args.Compact {
		compactTools(v.Entries)
		v.Commands = nil
	}
	return v, nil
}

func (a *app) exportCommand(ctx context.Context, cmd pluginsdk.CommandRun, args exportArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return exportResult{Dir: args.Dir}, a.export(ctx, s, args.Dir)
}

func (a *app) importCommand(ctx context.Context, cmd pluginsdk.CommandRun, args importArgs) (any, error) {
	res, err := a.importSession(ctx, a.host, cmd.WorkspaceID, origin(cmd), args)
	if err != nil {
		return nil, err
	}
	if !cmd.Agent {
		if err := a.selectSession(ctx, cmd.WorkspaceID, res.Session); err != nil {
			return nil, err
		}
	}
	a.kick(cmd.WorkspaceID)
	return res, nil
}

func (a *app) handoffCommand(ctx context.Context, cmd pluginsdk.CommandRun, args handoffArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if cmd.Agent && !a.descendantOf(args.Session, cmd.Caller) {
		return nil, errNotDescendant
	}
	next, err := a.handoff(ctx, s, args, origin(cmd))
	if err != nil {
		return nil, err
	}
	if !cmd.Agent {
		if err := a.selectSession(ctx, cmd.WorkspaceID, next.snapshot().ID); err != nil {
			return nil, err
		}
	}
	return handoffResult{Session: next.snapshot().ID}, nil
}

func (a *app) cancelCommand(_ context.Context, cmd pluginsdk.CommandRun, s *session) (any, error) {
	if cmd.Agent && !a.descendantOf(s.snapshot().ID, cmd.Caller) {
		return nil, errNotDescendant
	}
	a.cancel(s)
	return nil, nil
}

func (a *app) stopSubagentCommand(_ context.Context, cmd pluginsdk.CommandRun, args subagentArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return nil, s.stopSubagent(args.Subagent)
}

func (a *app) closeCommand(ctx context.Context, _ pluginsdk.CommandRun, s *session) (any, error) {
	a.closeTree(ctx, s)
	return nil, nil
}

func (a *app) deleteCommand(ctx context.Context, _ pluginsdk.CommandRun, s *session) (any, error) {
	return nil, a.deleteTree(ctx, s)
}

func (a *app) resumeCommand(_ context.Context, _ pluginsdk.CommandRun, s *session) (any, error) {
	a.resume(s)
	return nil, nil
}

func (a *app) revealCommand(ctx context.Context, cmd pluginsdk.CommandRun, s *session) (any, error) {
	return nil, a.reveal(ctx, cmd.WorkspaceID, s.snapshot().ID)
}

func (n newArgs) restricted() bool {
	return len(n.DisallowedTools) > 0 || n.SystemPrompt != "" || n.Mode != "" || n.Model != ""
}

func asUser(cmd pluginsdk.CommandRun) bool {
	return !cmd.Agent && (cmd.Via == remotePlugin || cmd.Via == voicePlugin)
}

func origin(cmd pluginsdk.CommandRun) string {
	if cmd.Caller == "" && cmd.Agent {
		return unknownCaller
	}
	return cmd.Caller
}

func (a *app) searchCommand(_ context.Context, cmd pluginsdk.CommandRun, args searchArgs) (any, error) {
	return a.search(cmd.WorkspaceID, args.Query), nil
}
