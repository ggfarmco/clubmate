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
	"workspace.worktree.set": {
		cmd: userOnly(coreCommand("workspace.worktree.set", "Set Folder Worktree", "Point a workspace folder at a git worktree directory; an empty path, or the folder itself, goes back to the folder.", &schema.Object{
			Properties: map[string]schema.Prop{
				"id":   {Type: "string", Description: "Workspace id."},
				"repo": {Type: "string", Description: "The workspace folder, exactly as listed in its repos."},
				"path": {Type: "string", Description: "Absolute path of an existing directory, usually a git worktree of the folder; empty to clear."},
			},
			Required: []string{"id", "repo", "path"},
		})),
		method: "workspace.worktree.set",
		params: passArgs,
	},
	"diagnostics.collect": {
		cmd:    userOnly(coreCommand("diagnostics.collect", "Collect Diagnostics", "Pack versions, log tails, plugin, workspace, session and UI state into a zip in the core's state dir.", nil)),
		method: "diagnostics.collect",
		params: withProject,
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
		cmd:    coreCommand("process.tree", "Process Tree", "List the processes of every session and of processes plugins register (e.g. agents), flagging orphans and duplicates.", nil),
		method: "process.tree",
	},
	"notify.post": {
		cmd: coreCommand("notify.post", "Post Notification", "Show a macOS notification for the workspace, e.g. when you need the user's input or finished a long task. Skipped while the user is looking at the session; at most 5 per 10 s per workspace.", &schema.Object{
			Properties: map[string]schema.Prop{
				"message": {Type: "string", Description: "Notification text, up to 1000 characters (longer is cut)."},
				"title":   {Type: "string", Description: "Short title, up to 120 characters."},
				"kind":    {Type: "string", Enum: kindEnum(), Description: "What the notification is about (default info)."},
				"session": {Type: "string", Description: "Session id to reveal when the notification is clicked: a terminal session (ses_…) or an agent session (agt_…)."},
			},
			Required: []string{"message"},
		}),
		method:       notifyMethod,
		needsProject: true,
		params:       withProject,
	},
	"testapp.call": {
		cmd: coreCommand("testapp.call", "Call Test Instance", "Call a method on an isolated Clubmate test instance started with scripts/test-app.sh: ui.snapshot {out?, png?, width?, height?} draws its windows to png plus a flagged layout dump (out defaults to /tmp/clubmate-test-<instance>/snapshots and must stay inside that instance dir), ui.present {target, shown} opens a popover or sheet or, with target card:<node id>, expands a transcript tool card, ui.key {keys, focus?} focuses a node and sends key combos such as opt+return to its key monitors, view shortcuts or first responder, ui.get/ui.event/command.list/command.run (always as an agent call)/project.list/workspace.open drive it.", &schema.Object{
			Properties: map[string]schema.Prop{
				"instance": {Type: "string", Description: "Test instance name, as printed by scripts/test-app.sh start or listed by testapp.list."},
				"method":   {Type: "string", Enum: testAppMethodList(), Description: "Method to call on the instance's core."},
				"params":   {Type: "object", Description: "The method's params."},
			},
			Required: []string{"instance", "method"},
		}),
		method: "testapp.call",
		params: passArgs,
	},
	"testapp.list": {
		cmd:    coreCommand("testapp.list", "List Test Instances", "List running Clubmate test instances started with scripts/test-app.sh.", nil),
		method: "testapp.list",
	},
	"ui.focus": {
		cmd: coreCommand("ui.focus", "Focus Section", "Show a section in the workspace's window.", &schema.Object{
			Properties: map[string]schema.Prop{
				"section": {Type: "string", Description: "Section id, e.g. editor-nvim, terminals, agents."},
				"slot":    {Type: "string", Description: "Slot to focus: <section>.sidebar or <section>.main; empty shows the section only."},
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

func userOnly(c Command) Command {
	c.Agent = false
	return c
}

func PrimitiveCommands() []Command {
	return primitiveCommands()
}

func primitiveCommands() []Command {
	out := make([]Command, 0, len(primitives))
	for _, p := range primitives {
		c := p.cmd
		c.Running = true
		out = append(out, c)
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
