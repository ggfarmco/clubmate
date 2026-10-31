package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/internal/rpc"
	"clubmate/internal/schema"
)

const (
	ProtocolVersion = "2025-06-18"

	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603

	workspaceArg = "workspace"

	baseInstructions = "Tools drive the Clubmate app: workspaces, sessions (terminals), the editor and plugin actions. Destructive tools wait for the user's approval in the app."
)

var (
	ErrNoCore = errors.New("Clubmate isn't running")

	supportedVersions = []string{ProtocolVersion, "2025-03-26", "2024-11-05"}
	readOnly          = []string{"workspace.list", "session.list", "session.read", "process.tree", "terminals.busy"}
	toolName          = strings.NewReplacer(".", "_", "-", "_")
	validName         = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
)

type Core interface {
	Call(ctx context.Context, method string, params, result any) error
	Close() error
}

type Server struct {
	Dial      func(ctx context.Context) (Core, error)
	Version   string
	Project   string
	Log       io.Writer
	Poll      time.Duration
	Caller    string
	NvimGuide string

	writeMu sync.Mutex
	out     io.Writer

	listMu sync.Mutex
	served []byte
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *protocolError  `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations annotations    `json:"annotations"`
	target      string
}

type annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
}

type callParams struct {
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func (e *protocolError) Error() string {
	return e.Message
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out
	watchCtx, stopWatch := context.WithCancel(ctx)
	var watcher sync.WaitGroup
	if s.Poll > 0 {
		watcher.Go(func() { s.watch(watchCtx) })
	}
	defer watcher.Wait()
	defer stopWatch()
	var handlers sync.WaitGroup
	defer handlers.Wait()
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			handlers.Go(func() { s.handle(ctx, line) })
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(json.RawMessage("null"), nil, &protocolError{Code: codeParse, Message: "parse error"})
		return
	}
	isRequest := len(req.ID) > 0 && string(req.ID) != "null"
	if !isRequest {
		return
	}
	if req.Method == "" && (req.Result != nil || req.Error != nil) {
		return
	}
	if req.Method == "" {
		s.reply(req.ID, nil, &protocolError{Code: codeInvalidRequest, Message: "missing method"})
		return
	}
	result, err := s.dispatch(ctx, req)
	var perr *protocolError
	if errors.As(err, &perr) {
		s.reply(req.ID, nil, perr)
		return
	}
	if err != nil {
		s.reply(req.ID, nil, &protocolError{Code: codeInternal, Message: err.Error()})
		return
	}
	s.reply(req.ID, result, nil)
}

func (s *Server) instructions() string {
	if _, err := os.Stat(s.NvimGuide); s.NvimGuide == "" || err != nil {
		return baseInstructions
	}
	return baseInstructions + " To customise Clubmate's Neovim, read " + s.NvimGuide + " first."
}

func (s *Server) dispatch(ctx context.Context, req request) (any, error) {
	switch req.Method {
	case "initialize":
		var p initializeParams
		_ = json.Unmarshal(req.Params, &p)
		version := ProtocolVersion
		if slices.Contains(supportedVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "clubmate", "version": s.Version},
			"instructions":    s.instructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools, err := s.tools(ctx)
		if err != nil {
			return nil, err
		}
		s.remember(tools)
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p callParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &protocolError{Code: codeInvalidParams, Message: fmt.Sprintf("decode params: %v", err)}
		}
		return s.call(ctx, p)
	}
	return nil, &protocolError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
}

func (s *Server) call(ctx context.Context, p callParams) (any, error) {
	core, err := s.Dial(ctx)
	if err != nil {
		return failure(err), nil
	}
	defer core.Close()
	tools, err := list(ctx, core, s.logf)
	if err != nil {
		return failure(err), nil
	}
	i := slices.IndexFunc(tools, func(t tool) bool { return t.Name == p.Name })
	if i < 0 {
		return nil, &protocolError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	workspace := ""
	if raw, ok := p.Arguments[workspaceArg]; ok {
		if err := json.Unmarshal(raw, &workspace); err != nil {
			return failure(fmt.Errorf("%s must be a string", workspaceArg)), nil
		}
		delete(p.Arguments, workspaceArg)
	}
	if workspace == "" {
		workspace = s.Project
	}
	if workspace == "" {
		workspace = mostRecent(ctx, core)
	}
	args := p.Arguments
	if args == nil {
		args = map[string]json.RawMessage{}
	}
	var out json.RawMessage
	params := map[string]any{"id": tools[i].target, "projectId": workspace, "args": args, "agent": true}
	if s.Caller != "" {
		params["caller"] = s.Caller
	}
	if err := core.Call(ctx, "command.run", params, &out); err != nil {
		if workspace == "" {
			err = fmt.Errorf("%w (no workspace: pass %q or open one in Clubmate)", err, workspaceArg)
		}
		return failure(err), nil
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return callResult{Content: []content{{Type: "text", Text: string(out)}}}, nil
}

func (s *Server) tools(ctx context.Context) ([]tool, error) {
	core, err := s.Dial(ctx)
	if errors.Is(err, ErrNoCore) {
		return build(rpc.PrimitiveCommands(), nil, s.logf), nil
	}
	if err != nil {
		return nil, err
	}
	defer core.Close()
	return list(ctx, core, s.logf)
}

func (s *Server) remember(tools []tool) {
	sig, _ := json.Marshal(tools)
	s.listMu.Lock()
	defer s.listMu.Unlock()
	s.served = sig
}

func (s *Server) watch(ctx context.Context) {
	tick := time.NewTicker(s.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.listMu.Lock()
		served := s.served
		s.listMu.Unlock()
		if served == nil {
			continue
		}
		tools, err := s.tools(ctx)
		if err != nil {
			continue
		}
		if sig, _ := json.Marshal(tools); !bytes.Equal(sig, served) {
			s.remember(tools)
			s.write(notification{JSONRPC: "2.0", Method: "notifications/tools/list_changed"})
		}
	}
}

func list(ctx context.Context, core Core, logf func(string, ...any)) ([]tool, error) {
	var cmds []rpc.Command
	if err := core.Call(ctx, "command.list", struct{}{}, &cmds); err != nil {
		return nil, err
	}
	var caps struct {
		Capabilities map[string]string `json:"capabilities"`
	}
	if err := core.Call(ctx, "command.capabilities", struct{}{}, &caps); err != nil {
		return nil, err
	}
	return build(cmds, caps.Capabilities, logf), nil
}

func build(cmds []rpc.Command, caps map[string]string, logf func(string, ...any)) []tool {
	byID := map[string]rpc.Command{}
	for _, c := range cmds {
		byID[c.ID] = c
	}
	covered := map[string]bool{}
	for _, id := range caps {
		covered[id] = true
	}
	var out []tool
	seen := map[string]string{}
	add := func(target string, c rpc.Command) {
		t := newTool(target, c)
		if !validName.MatchString(t.Name) {
			logf("tool %s skipped: invalid MCP tool name %q", target, t.Name)
			return
		}
		if c.Args != nil {
			if _, ok := c.Args.Properties[workspaceArg]; ok {
				logf("tool %s skipped: it declares an arg named %q, which MCP calls reserve", target, workspaceArg)
				return
			}
		}
		if first, ok := seen[t.Name]; ok {
			logf("tool %s: %s and %s map to the same name; using %s", t.Name, first, target, first)
			return
		}
		seen[t.Name] = target
		out = append(out, t)
	}
	for _, c := range cmds {
		if c.Agent && !covered[c.ID] {
			add(c.ID, c)
		}
	}
	names := make([]string, 0, len(caps))
	for name := range caps {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if c, ok := byID[caps[name]]; ok && c.Agent {
			add(name, c)
		}
	}
	return out
}

func newTool(target string, c rpc.Command) tool {
	description := c.Title
	if c.Description != "" {
		description += ". " + c.Description
	}
	return tool{
		Name:        toolName.Replace(target),
		Title:       c.Title,
		Description: description,
		InputSchema: inputSchema(c.Args),
		Annotations: annotations{Title: c.Title, ReadOnlyHint: slices.Contains(readOnly, c.ID), DestructiveHint: c.Destructive},
		target:      target,
	}
}

func inputSchema(args *schema.Object) map[string]any {
	props := map[string]any{}
	var required []string
	if args != nil {
		for name, p := range args.Properties {
			props[name] = p
		}
		required = args.Required
	}
	props[workspaceArg] = schema.Prop{Type: "string", Description: "Workspace id. Defaults to $CLUBMATE_PROJECT_ID, else the most recently opened workspace; ignored by tools that don't act on a workspace."}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func mostRecent(ctx context.Context, core Core) string {
	var projects []struct {
		ID string `json:"id"`
	}
	if err := core.Call(ctx, "project.list", struct{}{}, &projects); err != nil || len(projects) == 0 {
		return ""
	}
	return projects[0].ID
}

func failure(err error) callResult {
	return callResult{Content: []content{{Type: "text", Text: err.Error()}}, IsError: true}
}

func (s *Server) reply(id json.RawMessage, result any, perr *protocolError) {
	if perr == nil && result == nil {
		result = map[string]any{}
	}
	s.write(response{JSONRPC: "2.0", ID: id, Result: result, Error: perr})
}

func (s *Server) write(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		s.logf("encode message: %v", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.out.Write(append(raw, '\n')); err != nil {
		s.logf("write message: %v", err)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "clubmate mcp: "+format+"\n", args...)
	}
}
