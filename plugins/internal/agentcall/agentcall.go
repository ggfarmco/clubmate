package agentcall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"clubmate/internal/schema"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const corePlugin = "core"

var ErrUnknownCommand = errors.New("unknown command")

type target struct {
	ID     string         `json:"id"`
	Plugin string         `json:"plugin"`
	Agent  bool           `json:"agent"`
	Args   *schema.Object `json:"args"`
}

func Sessions(ctx context.Context, h *pluginsdk.Host, workspace string) ([]api.Session, error) {
	var sessions []api.Session
	if err := h.RunCommand(ctx, "agents.sessions", workspace, struct{}{}, &sessions); err != nil {
		return nil, fmt.Errorf("agent sessions of %s: %w", workspace, err)
	}
	return sessions, nil
}

func Reveal(ctx context.Context, h *pluginsdk.Host, workspace, session string) error {
	if err := h.RunCommand(ctx, "agents.reveal", workspace, map[string]string{"session": session}, nil); err != nil {
		return fmt.Errorf("reveal agent session %s: %w", session, err)
	}
	return nil
}

func Find(sessions []api.Session, id string) (api.Session, bool) {
	i := slices.IndexFunc(sessions, func(s api.Session) bool { return s.ID == id })
	if i < 0 {
		return api.Session{}, false
	}
	return sessions[i], true
}

func Parents(sessions []api.Session) map[string]string {
	parents := make(map[string]string, len(sessions))
	for _, s := range sessions {
		if s.ParentSession != "" {
			parents[s.ID] = s.ParentSession
		}
	}
	return parents
}

func Descends(session, ancestor string, parents map[string]string) bool {
	for range len(parents) + 1 {
		if session == "" {
			return false
		}
		if session == ancestor {
			return true
		}
		session = parents[session]
	}
	return false
}

func ValidateTarget(ctx context.Context, h *pluginsdk.Host, command, args string) (json.RawMessage, error) {
	var cmds []target
	if err := h.Call(ctx, "command.list", struct{}{}, &cmds); err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	i := slices.IndexFunc(cmds, func(c target) bool { return c.ID == command })
	if i < 0 {
		return nil, fmt.Errorf("%w %q", ErrUnknownCommand, command)
	}
	if !cmds[i].Agent {
		return nil, fmt.Errorf("command %q is not agent-visible", command)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return nil, fmt.Errorf("args: invalid JSON object: %w", err)
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(cmds[i].Args, raw); err != nil {
		return nil, err
	}
	if cmds[i].Plugin == corePlugin || cmds[i].Args != nil {
		if path := schema.Undeclared(cmds[i].Args, fields); path != "" {
			return nil, fmt.Errorf("%s: not allowed in agent calls", path)
		}
	}
	return raw, nil
}
