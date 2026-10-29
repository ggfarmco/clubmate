package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"clubmate/internal/core"
	"clubmate/internal/schema"
	"clubmate/internal/session"
)

const corePlugin = "core"

type primitive struct {
	cmd          Command
	method       string
	needsProject bool
	params       func(projectID string, args json.RawMessage) (json.RawMessage, error)
	scope        func(ctx context.Context, svc *core.Service, projectID string, args json.RawMessage) (string, error)
}

var primitives = map[string]primitive{
	"workspace.list": {
		cmd:    coreCommand("workspace.list", "List Workspaces", "List the workspaces, most recently opened first.", nil),
		method: "project.list",
	},
	"workspace.open": {
		cmd: coreCommand("workspace.open", "Open Workspace", "Open the workspace made of exactly these folders in the app, creating it when none matches.", &schema.Object{
			Properties: map[string]schema.Prop{
				"folders": {Type: "array", Items: &schema.Prop{Type: "string"}, Description: "Absolute paths of existing directories."},
			},
			Required: []string{"folders"},
		}),
		method: "workspace.open",
		params: passArgs,
	},
	"workspace.home": {
		cmd:    coreCommand("workspace.home", "Open Home Workspace", "Open the home workspace (rooted at the user's home directory) in the app.", nil),
		method: "workspace.home",
	},
	"session.list": {
		cmd:    coreCommand("session.list", "List Sessions", "List the terminal and plugin sessions of all workspaces.", nil),
		method: "session.list",
	},
	"session.create": {
		cmd: coreCommand("session.create", "Create Session", "Start a login-shell session in the workspace.", &schema.Object{
			Properties: map[string]schema.Prop{
				"cwd":   {Type: "string", Description: "Working directory; the first workspace folder (or home) when absent."},
				"title": {Type: "string", Description: "Session title."},
			},
		}),
		method:       "session.create",
		needsProject: true,
		params:       withProject,
	},
	"session.kill": {
		cmd: destructive(coreCommand("session.kill", "Kill Session", "Kill a session's process group.", &schema.Object{
			Properties: map[string]schema.Prop{
				"id": {Type: "string", Description: "Session id."},
			},
			Required: []string{"id"},
		})),
		method: "session.kill",
		params: passArgs,
		scope:  sessionProject,
	},
	"session.send": {
		cmd: destructive(coreCommand("session.send", "Send to Session", "Type text into a session, optionally pressing Enter. Agents need the user's approval; send long input in chunks.", &schema.Object{
			Properties: map[string]schema.Prop{
				"id":    {Type: "string", Description: "Session id."},
				"text":  {Type: "string", Description: "Text to type."},
				"enter": {Type: "boolean", Description: "Press Enter after the text."},
			},
			Required: []string{"id", "text"},
		})),
		method: "session.send",
		params: passArgs,
		scope:  sessionProject,
	},
	"session.read": {
		cmd: coreCommand("session.read", "Read Session", "Read the tail of a session's output, without ANSI escapes unless raw. A full-screen app gives altScreen true and no text.", &schema.Object{
			Properties: map[string]schema.Prop{
				"id":    {Type: "string", Description: "Session id."},
				"bytes": {Type: "integer", Description: "Bytes of output to read from the end (default 16384, max 262144)."},
				"raw":   {Type: "boolean", Description: "Keep ANSI escapes."},
			},
			Required: []string{"id"},
		}),
		method: "session.read",
		params: passArgs,
	},
	"process.tree": {
		cmd:    coreCommand("process.tree", "Process Tree", "List the processes of every session, flagging orphans and duplicates.", nil),
		method: "process.tree",
	},
	"ui.focus": {
		cmd: coreCommand("ui.focus", "Focus Section", "Show a section in the workspace's window.", &schema.Object{
			Properties: map[string]schema.Prop{
				"section": {Type: "string", Description: "Section id, e.g. editor-nvim, terminals, git."},
			},
			Required: []string{"section"},
		}),
		method:       "ui.focus",
		needsProject: true,
		params:       withProject,
	},
}

func coreCommand(id, title, description string, args *schema.Object) Command {
	return Command{ID: id, Title: title, Description: description, Plugin: corePlugin, Hidden: true, Args: args, Agent: true}
}

func destructive(c Command) Command {
	c.Destructive = true
	return c
}

func PrimitiveCommands() []Command {
	return primitiveCommands()
}

func primitiveCommands() []Command {
	out := make([]Command, 0, len(primitives))
	for _, p := range primitives {
		out = append(out, p.cmd)
	}
	slices.SortFunc(out, func(a, b Command) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func passArgs(_ string, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

func withProject(projectID string, args json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &fields); err != nil {
			return nil, invalidParams("args: want object")
		}
	}
	id, err := json.Marshal(projectID)
	if err != nil {
		return nil, err
	}
	fields["projectId"] = id
	return json.Marshal(fields)
}

func sessionProject(ctx context.Context, svc *core.Service, _ string, args json.RawMessage) (string, error) {
	var p idParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", invalidParams(fmt.Sprintf("args: %v", err))
	}
	if err := requireID(session.IDPrefix, p.ID); err != nil {
		return "", err
	}
	s, err := svc.GetSession(ctx, p.ID)
	if err != nil {
		return "", err
	}
	return s.ProjectID, nil
}

func sessionArg(isPrimitive bool) string {
	if isPrimitive {
		return "id"
	}
	return "sessionId"
}

func exactArgs(c Command, args json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &fields); err != nil {
			return nil, invalidParams("args: want object")
		}
	}
	if path := schema.Undeclared(c.Args, fields); path != "" {
		return nil, invalidParams(path + ": not allowed in agent calls")
	}
	return json.Marshal(fields)
}

func (s *Server) runPrimitive(ctx context.Context, p primitive, projectID string, args json.RawMessage) (json.RawMessage, error) {
	params := json.RawMessage("{}")
	if p.params != nil {
		built, err := p.params(projectID, args)
		if err != nil {
			return nil, err
		}
		params = built
	}
	out, err := s.methods[p.method](ctx, &params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
