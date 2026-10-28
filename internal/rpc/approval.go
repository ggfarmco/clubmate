package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/sourcegraph/jsonrpc2"
)

const (
	approvalTimeout   = 60 * time.Second
	approvalMaxFields = 10
	approvalMaxValue  = 200

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
}

type confirmResult struct {
	Approved bool `json:"approved"`
}

func (e deniedError) Error() string {
	return string(e)
}

func (deniedError) Is(target error) bool {
	return target == ErrDenied
}

func (s *Server) approve(ctx context.Context, c Command, projectID string, args json.RawMessage) error {
	workspace := ""
	if projectID != "" {
		p, err := s.svc.GetProject(ctx, projectID)
		if err != nil {
			return err
		}
		workspace = p.Name
	}
	args, err := approvableArgs(c, args)
	if err != nil {
		return err
	}
	conn, release, err := s.approver(projectID)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, approvalTimeout)
	defer cancel()
	var res confirmResult
	if err := conn.Call(ctx, callUIConfirm, confirmParams{Title: c.Title, Workspace: workspace, Command: c.ID, Args: args}, &res); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errApprovalTimeout
		}
		return errNoApprover
	}
	if !res.Approved {
		return errDeniedByUser
	}
	return nil
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
	for name, v := range fields {
		if _, ok := c.Args.Properties[name]; !ok {
			return nil, invalidParams(fmt.Sprintf("args.%s: not declared", name))
		}
		if renderedLen(v) > approvalMaxValue {
			return nil, invalidParams(fmt.Sprintf("args.%s: longer than %d characters", name, approvalMaxValue))
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
