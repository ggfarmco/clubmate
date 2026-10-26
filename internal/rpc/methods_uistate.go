package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourcegraph/jsonrpc2"
)

const (
	clientOwner   = "@client"
	maxUIStateLen = 64 << 10
)

type uiStateGetParams struct {
	ProjectID string `json:"projectId"`
}

type uiStateSetParams struct {
	ProjectID string          `json:"projectId"`
	State     json.RawMessage `json:"state"`
}

type uiStateResult struct {
	State json.RawMessage `json:"state"`
}

func (s *Server) uiStateMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"ui.state.get": method(func(ctx context.Context, p uiStateGetParams) (uiStateResult, error) {
			if err := s.checkUIStateProject(ctx, p.ProjectID); err != nil {
				return uiStateResult{}, err
			}
			value, found, err := s.kv.Get(ctx, clientOwner, p.ProjectID)
			if err != nil {
				return uiStateResult{}, err
			}
			if !found {
				value = json.RawMessage("null")
			}
			return uiStateResult{State: value}, nil
		}),
		"ui.state.set": method(func(ctx context.Context, p uiStateSetParams) (empty, error) {
			if err := s.checkUIStateProject(ctx, p.ProjectID); err != nil {
				return empty{}, err
			}
			if err := validateUIState(p.State); err != nil {
				return empty{}, err
			}
			return empty{}, s.kv.Set(ctx, clientOwner, p.ProjectID, p.State)
		}),
	}
}

func (s *Server) checkUIStateProject(ctx context.Context, projectID string) error {
	if CallerFrom(ctx).IsPlugin() {
		return &jsonrpc2.Error{Code: codeInvalidParams, Message: "ui state requires a client caller"}
	}
	if s.kv == nil {
		return errNoKV
	}
	if err := requireID(projectPrefix, projectID); err != nil {
		return err
	}
	_, err := s.svc.GetProject(ctx, projectID)
	return err
}

func validateUIState(state json.RawMessage) error {
	if len(state) > maxUIStateLen {
		return &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("state exceeds %d bytes", maxUIStateLen)}
	}
	trimmed := bytes.TrimSpace(state)
	if !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
		return &jsonrpc2.Error{Code: codeInvalidParams, Message: "state must be a JSON object"}
	}
	return nil
}

func (s *Server) deleteUIState(ctx context.Context, projectID string) error {
	if s.kv == nil {
		return nil
	}
	return s.kv.Delete(ctx, clientOwner, projectID)
}
