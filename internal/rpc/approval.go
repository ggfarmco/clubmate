package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

const (
	approvalTimeout = 60 * time.Second

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
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
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
