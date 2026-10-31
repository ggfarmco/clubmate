package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"clubmate/pkg/pluginsdk"
)

const (
	remotePlugin  = "remote"
	unknownCaller = "unknown"
)

var (
	commandTable = map[string]commandFunc{
		"agents.new":           withArgs((*app).newCommand),
		"agents.prompt":        withArgs((*app).promptCommand),
		"agents.answer":        withArgs((*app).answerCommand),
		"agents.permission":    withArgs((*app).permissionCommand),
		"agents.mode":          withArgs((*app).modeCommand),
		"agents.config":        withArgs((*app).configCommand),
		"agents.sessions":      (*app).sessionsCommand,
		"agents.notify":        withArgs((*app).notifyCommand),
		"agents.finishDefault": withArgs((*app).finishDefaultCommand),
		"agents.afk":           withArgs((*app).afkCommand),
		"agents.afkDefault":    withArgs((*app).afkDefaultCommand),
		"agents.stop-root":     withArgs((*app).stopRootCommand),
		"agents.wait":          withArgs((*app).waitCommand),
		"agents.transcript":    withArgs((*app).transcriptCommand),
		"agents.blame":         typed((*app).blameCommand),
		"agents.trace":         typed((*app).traceCommand),
		"agents.export":        withArgs((*app).exportCommand),
		"agents.import":        withArgs((*app).importCommand),
		"agents.handoff":       withArgs((*app).handoffCommand),
		"agents.cancel":        withSession((*app).cancelCommand),
		"agents.close":         withSession((*app).closeCommand),
		"agents.delete":        withSession((*app).deleteCommand),
		"agents.resume":        withSession((*app).resumeCommand),
		"agents.reveal":        withSession((*app).revealCommand),
	}

	errUnknownCmd    = errors.New("unknown command")
	errRelativeRoot  = errors.New("stop-root: root must be absolute")
	errNotDescendant = errors.New("the session was not started by this agent")
)

type commandFunc func(a *app, ctx context.Context, cmd pluginsdk.CommandRun) (any, error)

type sessionArgs struct {
	Session string `json:"session"`
}

type newArgs struct {
	Agent  string `json:"agent"`
	Branch string `json:"branch"`
	Prompt string `json:"prompt"`
	Title  string `json:"title"`
	Notify string `json:"notify"`
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
}

type attachment struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
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

type rootArgs struct {
	Root string `json:"root"`
}

type transcriptArgs struct {
	Session  string `json:"session"`
	FromTurn int    `json:"fromTurn"`
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
		s, err := a.lookup(cmd.ProjectID, args.Session)
		if err != nil {
			return nil, err
		}
		return fn(a, ctx, cmd, s)
	})
}

func (a *app) newCommand(ctx context.Context, cmd pluginsdk.CommandRun, args newArgs) (any, error) {
	res, err := a.newSession(ctx, a.host, cmd.ProjectID, origin(cmd), args)
	if err != nil {
		return nil, err
	}
	if res.parent == "" {
		if err := a.selectSession(ctx, cmd.ProjectID, res.Session); err != nil {
			return nil, err
		}
	}
	a.kick(cmd.ProjectID)
	return res, nil
}

func (a *app) promptCommand(_ context.Context, cmd pluginsdk.CommandRun, args promptArgs) (any, error) {
	args.From = origin(cmd)
	return nil, a.prompt(cmd.ProjectID, args)
}

func (a *app) answerCommand(_ context.Context, cmd pluginsdk.CommandRun, args answerArgs) (any, error) {
	return nil, a.prompt(cmd.ProjectID, promptArgs{Session: args.Session, Text: args.Text})
}

func (a *app) permissionCommand(_ context.Context, cmd pluginsdk.CommandRun, args permissionArgs) (any, error) {
	s, err := a.lookup(cmd.ProjectID, args.Session)
	if err != nil {
		return nil, err
	}
	user := !cmd.Agent && cmd.Via == remotePlugin
	if !user && (cmd.Caller == "" || !a.descendantOf(args.Session, cmd.Caller)) {
		return nil, errNotDescendant
	}
	return nil, s.answer(args.Request, args.Option)
}

func (a *app) modeCommand(ctx context.Context, cmd pluginsdk.CommandRun, args modeArgs) (any, error) {
	return nil, a.setMode(ctx, cmd.ProjectID, args)
}

func (a *app) configCommand(ctx context.Context, cmd pluginsdk.CommandRun, args configArgs) (any, error) {
	return nil, a.setConfig(ctx, cmd.ProjectID, args)
}

func (a *app) sessionsCommand(_ context.Context, cmd pluginsdk.CommandRun) (any, error) {
	return a.infos(cmd.ProjectID), nil
}

func (a *app) notifyCommand(_ context.Context, cmd pluginsdk.CommandRun, args notifyArgs) (any, error) {
	s, err := a.lookup(cmd.ProjectID, args.Session)
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
	if !validFinish(args.Value) {
		return finishResult{}, fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, args.Value)
	}
	return finishResult(args), a.host.StorageSet(ctx, finishKey, args.Value)
}

func (a *app) afkCommand(_ context.Context, cmd pluginsdk.CommandRun, args afkArgs) (any, error) {
	args.Session = cmp.Or(args.Session, cmd.Caller)
	s, err := a.lookup(cmd.ProjectID, args.Session)
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

func (a *app) stopRootCommand(ctx context.Context, _ pluginsdk.CommandRun, args rootArgs) (any, error) {
	if !filepath.IsAbs(args.Root) {
		return nil, errRelativeRoot
	}
	return nil, a.stopRoot(ctx, filepath.Clean(args.Root))
}

func (a *app) waitCommand(ctx context.Context, cmd pluginsdk.CommandRun, args waitArgs) (any, error) {
	return a.wait(ctx, cmd.ProjectID, cmd.Caller, args)
}

func (a *app) transcriptCommand(_ context.Context, cmd pluginsdk.CommandRun, args transcriptArgs) (any, error) {
	s, err := a.lookup(cmd.ProjectID, args.Session)
	if err != nil {
		return nil, err
	}
	v := s.view(args.FromTurn)
	if cmd.Agent {
		dropImageData(v.Entries)
	}
	return v, nil
}

func (a *app) exportCommand(ctx context.Context, cmd pluginsdk.CommandRun, args exportArgs) (any, error) {
	s, err := a.lookup(cmd.ProjectID, args.Session)
	if err != nil {
		return nil, err
	}
	return exportResult{Dir: args.Dir}, a.export(ctx, s, args.Dir)
}

func (a *app) importCommand(ctx context.Context, cmd pluginsdk.CommandRun, args importArgs) (any, error) {
	res, err := a.importSession(ctx, a.host, cmd.ProjectID, origin(cmd), args)
	if err != nil {
		return nil, err
	}
	if !cmd.Agent {
		if err := a.selectSession(ctx, cmd.ProjectID, res.Session); err != nil {
			return nil, err
		}
	}
	a.kick(cmd.ProjectID)
	return res, nil
}

func (a *app) handoffCommand(ctx context.Context, cmd pluginsdk.CommandRun, args handoffArgs) (any, error) {
	s, err := a.lookup(cmd.ProjectID, args.Session)
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
		if err := a.selectSession(ctx, cmd.ProjectID, next.snapshot().ID); err != nil {
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
	return nil, a.reveal(ctx, cmd.ProjectID, s.snapshot().ID)
}

func origin(cmd pluginsdk.CommandRun) string {
	if cmd.Caller == "" && cmd.Agent {
		return unknownCaller
	}
	return cmd.Caller
}
