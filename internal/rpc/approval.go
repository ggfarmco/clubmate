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
	approvalMaxInt    = 1 << 53

	callUIConfirm = "ui.confirm"
)

var (
	errNoApprover      = deniedError("no app to approve")
	errDeniedByUser    = deniedError("denied by user")
	errApprovalBusy    = deniedError("approval busy")
	errApprovalTimeout = deniedError("approval timed out")
)

type deniedError string

type confirmParams struct {
	Title     string          `json:"title"`
	Workspace string          `json:"workspace"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	TimeoutMs int64           `json:"timeoutMs"`
	Session   *confirmSession `json:"session,omitempty"`
}

type confirmSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
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
	ProjectID string            `json:"projectId"`
	Title     string            `json:"title"`
	Command   string            `json:"command"`
	Args      map[string]string `json:"args"`
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
	workspace, err := s.workspaceName(ctx, p.ProjectID)
	if err != nil {
		return empty{}, err
	}
	args, err := json.Marshal(p.Args)
	if err != nil {
		return empty{}, err
	}
	return empty{}, s.confirm(ctx, p.ProjectID, confirmParams{Title: p.Title, Workspace: workspace, Command: p.Command, Args: args})
}

func (s *Server) workspaceName(ctx context.Context, projectID string) (string, error) {
	if projectID == "" {
		return "", nil
	}
	p, err := s.svc.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	return p.Name, nil
}

func (s *Server) approve(ctx context.Context, c Command, projectID, sessionKey string, raw json.RawMessage) (json.RawMessage, error) {
	workspace, err := s.workspaceName(ctx, projectID)
	if err != nil {
		return nil, err
	}
	args, err := approvableArgs(c, raw)
	if err != nil {
		return nil, err
	}
	params := confirmParams{Title: c.Title, Workspace: workspace, Command: c.ID, Args: args, Session: s.targetSession(ctx, sessionKey, args)}
	if err := s.confirm(ctx, projectID, params); err != nil {
		return nil, err
	}
	return args, nil
}

func (s *Server) confirm(ctx context.Context, projectID string, params confirmParams) error {
	conn, release, err := s.approver(projectID)
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
	if p, err := s.svc.GetProject(ctx, sess.ProjectID); err == nil {
		out.Workspace = p.Name
	}
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

func (s *Server) approver(projectID string) (*jsonrpc2.Conn, func(), error) {
	present := map[*jsonrpc2.Conn]bool{}
	if projectID != "" {
		for _, t := range s.presence.focusTargets(projectID) {
			present[t.conn] = true
		}
	}
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	var (
		best     *jsonrpc2.Conn
		bestInfo *client
	)
	for conn, c := range s.clients {
		if _, ok := c.handles[handleUIConfirm]; !ok {
			continue
		}
		if best == nil || present[conn] && !present[best] || present[conn] == present[best] && c.seq < bestInfo.seq {
			best, bestInfo = conn, c
		}
	}
	if best == nil {
		return nil, nil, errNoApprover
	}
	if bestInfo.approving {
		return nil, nil, errApprovalBusy
	}
	bestInfo.approving = true
	return best, func() {
		s.clientsMu.Lock()
		defer s.clientsMu.Unlock()
		bestInfo.approving = false
	}, nil
}
