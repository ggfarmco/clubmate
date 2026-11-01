package main

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"clubmate/internal/schema"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	voiceTitle       = "Voice"
	voiceMode        = "bypassPermissions"
	notifyMuted      = "muted"
	sessionKeyPrefix = "voice.session."
	toolPrefix       = "mcp__clubmate__"
	corePlugin       = "core"
	terminalInput    = "session.send"
	allowAlways      = "allow_always"
	allowPrefix      = "allow"
	turnTimeout      = 90 * time.Second
	confirmTimeout   = 60 * time.Second

	msgNoAnswer     = "the assistant did not answer"
	resultCancelled = "cancelled"
	staleUnknown    = "unknown"
)

var (
	errNotVoiceSession   = errors.New("not the voice session")
	errNotAgentCommand   = errors.New("not an agent command")
	errNotDangerous      = errors.New("not dangerous, call it directly as a Clubmate MCP tool")
	errUtteranceOver     = errors.New("the utterance is over")
	errDeclined          = errors.New("the user declined")
	errNothingToConfirm  = errors.New("nothing to confirm")
	errConfirmPending    = errors.New("another action is waiting for the user's confirmation")
	errUnknownPermission = errors.New("no pending permission request with this id and option")
	errEmptyAnswer       = errors.New("give text, or request with answers or option")

	builtinTools   = []string{"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "Read", "Glob", "Grep", "WebFetch", "WebSearch", "Task", "Agent"}
	answerCommands = []string{"agents.answer", "agents.permission", "agents.elicitation"}
	pluginOnlyArgs = []string{"disallowedTools", "systemPrompt", "mode"}
	toolName       = strings.NewReplacer(".", "_", "-", "_")

	//go:embed prompt.md
	systemPrompt string
)

type stopper interface {
	Stop() bool
}

type commandDef struct {
	ID          string         `json:"id"`
	Plugin      string         `json:"plugin"`
	Agent       bool           `json:"agent"`
	Destructive bool           `json:"destructive"`
	Args        *schema.Object `json:"args"`
}

type newSessionArgs struct {
	Title           string   `json:"title"`
	Notify          string   `json:"notify"`
	Mode            string   `json:"mode"`
	SystemPrompt    string   `json:"systemPrompt"`
	DisallowedTools []string `json:"disallowedTools"`
}

type newSessionResult struct {
	Session string `json:"session"`
}

type promptArgs struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

type voicePrompt struct {
	Utterance string  `json:"utterance"`
	Device    string  `json:"device"`
	Heard     string  `json:"heard"`
	Context   Context `json:"context"`
}

type planArgs struct {
	Utterance string `json:"utterance"`
	Summary   string `json:"summary"`
}

type answerArgs struct {
	Utterance string   `json:"utterance"`
	Session   string   `json:"session"`
	Text      string   `json:"text"`
	Request   string   `json:"request"`
	Answers   []answer `json:"answers"`
	Option    string   `json:"option"`
}

type answer struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type runArgs struct {
	Utterance string `json:"utterance"`
	Command   string `json:"command"`
	Args      string `json:"args"`
	Summary   string `json:"summary"`
}

type doneArgs struct {
	Utterance string `json:"utterance"`
	Result    string `json:"result"`
}

type confirmArgs struct {
	Utterance string `json:"utterance"`
	Approve   bool   `json:"approve"`
}

type elicitationArgs struct {
	Session string   `json:"session"`
	Request string   `json:"request"`
	Action  string   `json:"action"`
	Answers []answer `json:"answers"`
}

type permissionArgs struct {
	Session string `json:"session"`
	Request string `json:"request"`
	Option  string `json:"option"`
}

func (v *voice) agentCommand(ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "voice.plan":
		var args planArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, v.plan(cmd, args)
	case "voice.answer":
		var args answerArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, v.answer(ctx, cmd, args)
	case "voice.run":
		var args runArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.run(ctx, cmd, args)
	case "voice.done":
		var args doneArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, v.done(cmd, args)
	}
	return nil, fmt.Errorf("unknown command %q", cmd.ID)
}

func (v *voice) plan(cmd pluginsdk.CommandRun, args planArgs) error {
	u, err := v.caller(cmd, args.Utterance)
	if err != nil {
		return err
	}
	if !v.update(u, func(s *State) { s.Plan = args.Summary }) {
		return errUtteranceOver
	}
	return nil
}

func (v *voice) answer(ctx context.Context, cmd pluginsdk.CommandRun, args answerArgs) error {
	u, err := v.caller(cmd, args.Utterance)
	if err != nil {
		return err
	}
	switch {
	case args.Request != "" && args.Option != "":
		p := permissionArgs{Session: args.Session, Request: args.Request, Option: args.Option}
		if err := v.confirmPermission(ctx, u, p); err != nil {
			return err
		}
		return v.h.RunCommand(ctx, "agents.permission", cmd.WorkspaceID, p, nil)
	case args.Request != "" && len(args.Answers) > 0:
		return v.h.RunCommand(ctx, "agents.elicitation", cmd.WorkspaceID, elicitationArgs{Session: args.Session, Request: args.Request, Action: "accept", Answers: args.Answers}, nil)
	case args.Request == "" && args.Text != "":
		return v.h.RunCommand(ctx, "agents.answer", cmd.WorkspaceID, promptArgs{Session: args.Session, Text: args.Text}, nil)
	}
	return errEmptyAnswer
}

func (v *voice) run(ctx context.Context, cmd pluginsdk.CommandRun, args runArgs) (json.RawMessage, error) {
	u, err := v.caller(cmd, args.Utterance)
	if err != nil {
		return nil, err
	}
	target, err := v.agentTarget(ctx, args.Command)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if strings.TrimSpace(args.Args) != "" {
		if err := json.Unmarshal([]byte(args.Args), &fields); err != nil {
			return nil, fmt.Errorf("args: invalid JSON object: %w", err)
		}
	}
	if err := checkArgs(target, fields); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := v.confirm(ctx, u, Confirm{Title: args.Summary, Command: target.ID, Args: raw}); err != nil {
		return nil, err
	}
	var out json.RawMessage
	if err := v.h.RunCommand(ctx, target.ID, cmd.WorkspaceID, json.RawMessage(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *voice) done(cmd pluginsdk.CommandRun, args doneArgs) error {
	u, err := v.caller(cmd, args.Utterance)
	if err != nil {
		return err
	}
	v.update(u, func(s *State) {
		s.Phase = phaseDone
		s.Result = args.Result
		s.Confirm = nil
	})
	return nil
}

func (v *voice) confirmed(workspace string, args confirmArgs) error {
	u := v.lookup(workspace, args.Utterance)
	if u == nil {
		return errUnknownUtterance
	}
	if !v.resolveConfirm(u, args.Approve) {
		return errNothingToConfirm
	}
	return nil
}

func (v *voice) routeAsync(u *utterance) {
	if err := v.prompt(v.ctx, u); err != nil {
		v.fail(u, err)
	}
}

func (v *voice) prompt(ctx context.Context, u *utterance) error {
	v.mu.Lock()
	text, err := json.Marshal(voicePrompt{Utterance: u.state.Utterance, Device: u.where.Device, Heard: u.state.Heard, Context: u.where})
	v.mu.Unlock()
	if err != nil {
		return err
	}
	sid, err := v.promptSession(ctx, u, string(text), "")
	if err == nil || errors.Is(err, errUtteranceOver) {
		return nil
	}
	logError("prompt the voice session "+sid, err)
	_, err = v.promptSession(ctx, u, string(text), cmp.Or(sid, staleUnknown))
	if errors.Is(err, errUtteranceOver) {
		return nil
	}
	return err
}

func (v *voice) promptSession(ctx context.Context, u *utterance, text, stale string) (string, error) {
	sid, err := v.voiceSession(ctx, u.workspace, stale)
	if err != nil {
		return "", err
	}
	if !v.update(u, func(s *State) { s.Session = sid }) {
		return sid, errUtteranceOver
	}
	v.armTurn(u)
	return sid, v.h.RunCommand(ctx, "agents.prompt", u.workspace, promptArgs{Session: sid, Text: text}, nil)
}

func (v *voice) voiceSession(ctx context.Context, workspace, stale string) (string, error) {
	v.sessionMu.Lock()
	defer v.sessionMu.Unlock()
	key := sessionKeyPrefix + workspace
	var sid string
	if _, err := v.h.StorageGet(ctx, key, &sid); err != nil {
		return "", err
	}
	if sid != "" && sid != stale {
		var sessions []api.Session
		if err := v.h.RunCommand(ctx, "agents.sessions", workspace, struct{}{}, &sessions); err != nil {
			return "", err
		}
		if slices.ContainsFunc(sessions, func(s api.Session) bool { return s.ID == sid }) {
			return sid, nil
		}
	}
	tools, err := v.dangerousTools(ctx)
	if err != nil {
		return "", err
	}
	args := newSessionArgs{
		Title:           voiceTitle,
		Notify:          notifyMuted,
		Mode:            voiceMode,
		SystemPrompt:    systemPrompt,
		DisallowedTools: append(slices.Clone(builtinTools), tools...),
	}
	var res newSessionResult
	if err := v.h.RunCommand(ctx, "agents.new", workspace, args, &res); err != nil {
		return "", fmt.Errorf("start the voice session: %w", err)
	}
	return res.Session, v.h.StorageSet(ctx, key, res.Session)
}

func (v *voice) dangerousTools(ctx context.Context) ([]string, error) {
	cmds, err := v.commands(ctx)
	if err != nil {
		return nil, err
	}
	tools := []string{toolPrefix + toolName.Replace(terminalInput)}
	for _, c := range cmds {
		if !c.Agent || !c.Destructive || c.ID == terminalInput || slices.Contains(answerCommands, c.ID) {
			continue
		}
		tools = append(tools, toolPrefix+toolName.Replace(c.ID))
	}
	return tools, nil
}

func (v *voice) agentTarget(ctx context.Context, id string) (commandDef, error) {
	cmds, err := v.commands(ctx)
	if err != nil {
		return commandDef{}, err
	}
	i := slices.IndexFunc(cmds, func(c commandDef) bool { return c.ID == id })
	if i < 0 {
		return commandDef{}, fmt.Errorf("unknown command %q", id)
	}
	c := cmds[i]
	if !c.Agent || strings.HasPrefix(c.ID, "voice.") {
		return commandDef{}, fmt.Errorf("%w: %s", errNotAgentCommand, id)
	}
	if slices.Contains(answerCommands, c.ID) {
		return commandDef{}, fmt.Errorf("%s: answer sessions with voice.answer", id)
	}
	if !c.Destructive && c.ID != terminalInput {
		return commandDef{}, fmt.Errorf("%w: %s", errNotDangerous, id)
	}
	return c, nil
}

func (v *voice) commands(ctx context.Context) ([]commandDef, error) {
	var cmds []commandDef
	if err := v.h.Call(ctx, "command.list", struct{}{}, &cmds); err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	return cmds, nil
}

func (v *voice) confirmPermission(ctx context.Context, u *utterance, p permissionArgs) error {
	var tr transcript
	if err := v.h.RunCommand(ctx, "agents.transcript", u.workspace, map[string]any{"session": p.Session, "compact": true}, &tr); err != nil {
		return err
	}
	for _, e := range tr.Entries {
		if e.Kind != entryPermission || e.ID != p.Request {
			continue
		}
		i := slices.IndexFunc(e.Options, func(o PermissionOption) bool { return o.OptionID == p.Option })
		if i < 0 {
			return fmt.Errorf("%w: %s", errUnknownPermission, p.Option)
		}
		title := "Allow: "
		switch kind := e.Options[i].Kind; {
		case kind == allowAlways:
			title = "Always allow: "
		case !strings.HasPrefix(kind, allowPrefix) || v.inContext(u, p):
			return nil
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return v.confirm(ctx, u, Confirm{Title: title + permissionTitle(e), Command: "agents.permission", Args: raw})
	}
	return fmt.Errorf("%w: %s", errUnknownPermission, p.Request)
}

func (v *voice) inContext(u *utterance, p permissionArgs) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := u.where.Session
	return s != nil && s.ID == p.Session && s.Permission != nil && s.Permission.Request == p.Request
}

func (v *voice) confirm(ctx context.Context, u *utterance, c Confirm) error {
	v.mu.Lock()
	busy := u.confirm != nil
	v.mu.Unlock()
	if busy {
		return errConfirmPending
	}
	ch := make(chan bool, 1)
	if !v.update(u, func(s *State) {
		s.Phase = phaseConfirm
		s.Confirm = &c
		u.confirm = ch
	}) {
		return errUtteranceOver
	}
	timer := v.after(confirmTimeout, func() { v.resolveConfirm(u, false) })
	defer timer.Stop()
	select {
	case approve := <-ch:
		if !approve {
			v.update(u, func(s *State) {
				s.Phase = phaseCancelled
				s.Result = resultCancelled
			})
			return errDeclined
		}
	case <-u.done:
		return errUtteranceOver
	case <-ctx.Done():
		return ctx.Err()
	}
	if !v.update(u, func(s *State) {
		s.Phase = phaseThinking
		s.Confirm = nil
	}) {
		return errUtteranceOver
	}
	v.armTurn(u)
	return nil
}

func (v *voice) resolveConfirm(u *utterance, approve bool) bool {
	v.mu.Lock()
	ch := u.confirm
	u.confirm = nil
	v.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- approve
	return true
}

func (v *voice) armTurn(u *utterance) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if u.turn != nil {
		u.turn.Stop()
	}
	if u.terminal() {
		return
	}
	u.turn = v.after(turnTimeout, func() { v.turnExpired(u) })
}

func (v *voice) turnExpired(u *utterance) {
	v.mu.Lock()
	waiting := u.state.Phase == phaseConfirm
	v.mu.Unlock()
	if waiting {
		v.armTurn(u)
		return
	}
	v.fail(u, errors.New(msgNoAnswer))
}

func (v *voice) caller(cmd pluginsdk.CommandRun, uid string) (*utterance, error) {
	u := v.lookup(cmd.WorkspaceID, uid)
	if u == nil {
		return nil, errUnknownUtterance
	}
	v.mu.Lock()
	sid := u.state.Session
	v.mu.Unlock()
	if !cmd.Agent || cmd.Caller == "" || cmd.Caller != sid {
		return nil, errNotVoiceSession
	}
	return u, nil
}

func checkArgs(c commandDef, fields map[string]json.RawMessage) error {
	if c.ID == "agents.new" {
		for _, name := range pluginOnlyArgs {
			if _, ok := fields[name]; ok {
				return fmt.Errorf("args.%s: not allowed in agent calls", name)
			}
		}
	}
	if c.Plugin != corePlugin && c.Args == nil {
		return nil
	}
	if path := schema.Undeclared(c.Args, fields); path != "" {
		return fmt.Errorf("%s: not allowed in agent calls", path)
	}
	return nil
}
