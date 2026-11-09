package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/schema"
)

const (
	approvalTimeout   = 60 * time.Second
	approvalMaxFields = 10
	approvalMaxValue  = 200
	approvalMaxBody   = 64 << 10
	approvalMaxInt    = 1 << 53
	callerLookup      = 2 * time.Second

	callUIConfirm     = "ui.confirm"
	cmdAgentsSessions = "agents.sessions"
)

var (
	errNoApprover      = deniedError("no app to approve")
	errDeniedByUser    = deniedError("denied by user")
	errApprovalBusy    = deniedError("approval busy")
	errApprovalTimeout = deniedError("approval timed out")
)

type deniedError string

type confirmParams struct {
	Title         string          `json:"title"`
	WorkspaceName string          `json:"workspaceName"`
	Command       string          `json:"command"`
	Args          json.RawMessage `json:"args"`
	Body          string          `json:"body,omitempty"`
	Plugin        bool            `json:"plugin,omitempty"`
	TimeoutMs     int64           `json:"timeoutMs"`
	Session       *confirmSession `json:"session,omitempty"`
	From          *confirmSession `json:"from,omitempty"`
}

type confirmSession struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	WorkspaceName string `json:"workspaceName"`
}

type confirmResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

func (e deniedError) Error() string {
	return string(e)
}

func (deniedError) Is(target error) bool {
	return target == ErrDenied
}

type approveParams struct {
	WorkspaceID string            `json:"workspaceId"`
	Title       string            `json:"title"`
	Command     string            `json:"command"`
	Args        map[string]string `json:"args"`
	Body        string            `json:"body"`
}

func (s *Server) uiApprove(ctx context.Context, p approveParams) (empty, error) {
	caller := CallerFrom(ctx)
	if !caller.IsPlugin() {
		return empty{}, invalidParams("ui.approve requires a plugin caller")
	}
	if p.Title == "" || utf8.RuneCountInString(p.Title) > approvalMaxValue {
		return empty{}, invalidParams(fmt.Sprintf("title: want 1 to %d characters", approvalMaxValue))
	}
	if !strings.HasPrefix(p.Command, caller.PluginID+".") {
		return empty{}, invalidParams(fmt.Sprintf("command: want a %s. command", caller.PluginID))
	}
	if len(p.Args) > approvalMaxFields {
		return empty{}, invalidParams(fmt.Sprintf("args: more than %d fields", approvalMaxFields))
	}
	for name, v := range p.Args {
		if utf8.RuneCountInString(v) > approvalMaxValue {
			return empty{}, invalidParams(fmt.Sprintf("args.%s: longer than %d characters", name, approvalMaxValue))
		}
	}
	if utf8.RuneCountInString(p.Body) > approvalMaxBody {
		return empty{}, invalidParams(fmt.Sprintf("body: longer than %d characters", approvalMaxBody))
	}
	name, err := s.workspaceName(ctx, p.WorkspaceID)
	if err != nil {
		return empty{}, err
	}
	args, err := json.Marshal(p.Args)
	if err != nil {
		return empty{}, err
	}
	return empty{}, s.confirm(ctx, p.WorkspaceID, confirmParams{Title: p.Title, WorkspaceName: name, Command: p.Command, Args: args, Body: p.Body, Plugin: true})
}

func (s *Server) workspaceName(ctx context.Context, workspaceID string) (string, error) {
	if workspaceID == "" {
		return "", nil
	}
	p, err := s.svc.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return p.Name, nil
}

func (s *Server) approve(ctx context.Context, c Command, workspaceID, sessionKey, caller string, raw json.RawMessage) (json.RawMessage, error) {
	name, err := s.workspaceName(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	args, err := approvableArgs(c, raw)
	if err != nil {
		return nil, err
	}
	params := confirmParams{Title: c.Title, WorkspaceName: name, Command: c.ID, Args: args, Session: s.targetSession(ctx, sessionKey, args), From: s.callerSession(ctx, workspaceID, caller)}
	if err := s.confirm(ctx, workspaceID, params); err != nil {
		return nil, err
	}
	return args, nil
}

func (s *Server) confirm(ctx context.Context, workspaceID string, params confirmParams) error {
	conn, release, err := s.approver(workspaceID)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, approvalTimeout)
	defer cancel()
	params.TimeoutMs = approvalTimeout.Milliseconds()
	var res confirmResult
	if err := conn.Call(ctx, callUIConfirm, params, &res); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errApprovalTimeout
		}
		return errNoApprover
	}
	if !res.Approved && res.Reason != "" {
		return deniedError(res.Reason)
	}
	if !res.Approved {
		return errDeniedByUser
	}
	return nil
}

func (s *Server) targetSession(ctx context.Context, key string, args json.RawMessage) *confirmSession {
	var fields map[string]json.RawMessage
	var id string
	if json.Unmarshal(args, &fields) != nil || json.Unmarshal(fields[key], &id) != nil {
		return nil
	}
	sess, err := s.svc.GetSession(ctx, id)
	if err != nil {
		return nil
	}
	out := &confirmSession{ID: sess.ID, Title: sess.Title}
	if p, err := s.svc.GetWorkspace(ctx, sess.WorkspaceID); err == nil {
		out.WorkspaceName = p.Name
	}
	return out
}

func (s *Server) callerSession(ctx context.Context, workspaceID, caller string) *confirmSession {
	if caller == "" {
		return nil
	}
	out := &confirmSession{ID: caller}
	ctx, cancel := context.WithTimeout(ctx, callerLookup)
	defer cancel()
	raw, err := s.runPluginCommand(ctx, cmdAgentsSessions, workspaceID, map[string]string{"session": caller})
	var list []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err != nil || json.Unmarshal(raw, &list) != nil || len(list) == 0 || list[0].ID != caller {
		return out
	}
	out.Title, _, _ = strings.Cut(list[0].Title, "\n")
	return out
}

func approvableArgs(c Command, raw json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, invalidParams("args: want object")
		}
	}
	if c.Args == nil && len(fields) > 0 {
		return nil, invalidParams(fmt.Sprintf("args: %s declares no args", c.ID))
	}
	if len(fields) > approvalMaxFields {
		return nil, invalidParams(fmt.Sprintf("args: more than %d fields", approvalMaxFields))
	}
	if path := schema.Undeclared(c.Args, fields); path != "" {
		return nil, invalidParams(path + ": not declared")
	}
	for name, v := range fields {
		if renderedLen(v) > approvalMaxValue {
			return nil, invalidParams(fmt.Sprintf("args.%s: longer than %d characters", name, approvalMaxValue))
		}
		if !exactNumbers(v) {
			return nil, invalidParams(fmt.Sprintf("args.%s: number outside ±2^53", name))
		}
	}
	return json.Marshal(fields)
}

func renderedLen(v json.RawMessage) int {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return utf8.RuneCountInString(s)
	}
	var buf bytes.Buffer
	if json.Compact(&buf, v) != nil {
		return utf8.RuneCount(v)
	}
	return utf8.RuneCount(buf.Bytes())
}

func exactNumbers(v json.RawMessage) bool {
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	var decoded any
	if dec.Decode(&decoded) != nil {
		return false
	}
	return exact(decoded)
}

func exact(v any) bool {
	switch v := v.(type) {
	case json.Number:
		n, ok := new(big.Float).SetString(v.String())
		return ok && n.Abs(n).Cmp(big.NewFloat(approvalMaxInt)) <= 0
	case []any:
		for _, item := range v {
			if !exact(item) {
				return false
			}
		}
	case map[string]any:
		for _, item := range v {
			if !exact(item) {
				return false
			}
		}
	}
	return true
}

func (s *Server) approver(workspaceID string) (*jsonrpc2.Conn, func(), error) {
	present := s.present(workspaceID)
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	best, info := s.pickClient(handleUIConfirm, present)
	if best == nil {
		return nil, nil, errNoApprover
	}
	if info.approving {
		return nil, nil, errApprovalBusy
	}
	info.approving = true
	return best, func() {
		s.clientsMu.Lock()
		defer s.clientsMu.Unlock()
		info.approving = false
	}, nil
}

func (s *Server) clientFor(handle, workspaceID string) (*jsonrpc2.Conn, error) {
	present := s.present(workspaceID)
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	best, _ := s.pickClient(handle, present)
	if best == nil {
		return nil, errNoBrowserApp
	}
	return best, nil
}

func (s *Server) present(workspaceID string) map[*jsonrpc2.Conn]bool {
	present := map[*jsonrpc2.Conn]bool{}
	if workspaceID != "" {
		for _, t := range s.presence.focusTargets(workspaceID) {
			present[t.conn] = true
		}
	}
	return present
}

func (s *Server) pickClient(handle string, present map[*jsonrpc2.Conn]bool) (*jsonrpc2.Conn, *client) {
	var (
		best     *jsonrpc2.Conn
		bestInfo *client
	)
	for conn, c := range s.clients {
		if _, ok := c.handles[handle]; !ok {
			continue
		}
		if best == nil || present[conn] && !present[best] || present[conn] == present[best] && c.seq < bestInfo.seq {
			best, bestInfo = conn, c
		}
	}
	return best, bestInfo
}
