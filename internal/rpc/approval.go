package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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

func (s *Server) approve(ctx context.Context, c Command, projectID, sessionKey string, raw json.RawMessage) (json.RawMessage, error) {
	workspace := ""
	if projectID != "" {
		p, err := s.svc.GetProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		workspace = p.Name
	}
	args, err := approvableArgs(c, raw)
	if err != nil {
		return nil, err
	}
	conn, release, err := s.approver(projectID)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, approvalTimeout)
	defer cancel()
	params := confirmParams{Title: c.Title, Workspace: workspace, Command: c.ID, Args: args, TimeoutMs: approvalTimeout.Milliseconds(), Session: s.targetSession(ctx, sessionKey, args)}
	var res confirmResult
	if err := conn.Call(ctx, callUIConfirm, params, &res); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errApprovalTimeout
		}
		return nil, errNoApprover
	}
	if !res.Approved && res.Reason != "" {
		return nil, deniedError(res.Reason)
	}
	if !res.Approved {
		return nil, errDeniedByUser
	}
	return args, nil
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
