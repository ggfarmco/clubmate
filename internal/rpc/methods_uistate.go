package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/contract"
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

type uiStatePatchParams struct {
	ProjectID string          `json:"projectId"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
}

type uiStateResult struct {
	State json.RawMessage `json:"state"`
}

type uiSectionResult struct {
	Section string `json:"section"`
}

func (s *Server) uiStateMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"ui.state.get": method(func(ctx context.Context, p uiStateGetParams) (uiStateResult, error) {
			if err := s.checkUIStateClient(ctx, p.ProjectID); err != nil {
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
			if err := s.checkUIStateClient(ctx, p.ProjectID); err != nil {
				return empty{}, err
			}
			if err := validateUIState(p.State); err != nil {
				return empty{}, err
			}
			return empty{}, s.setUIState(ctx, p.ProjectID, p.State)
		}),
		"ui.state.patch": method(func(ctx context.Context, p uiStatePatchParams) (empty, error) {
			if err := s.checkUIStateClient(ctx, p.ProjectID); err != nil {
				return empty{}, err
			}
			if p.Key == "" {
				return empty{}, &jsonrpc2.Error{Code: codeInvalidParams, Message: "key is required"}
			}
			return empty{}, s.patchUIState(ctx, p.ProjectID, p.Key, p.Value)
		}),
		"ui.state.section": method(func(ctx context.Context, p uiStateGetParams) (uiSectionResult, error) {
			if err := s.checkUIStateProject(ctx, p.ProjectID); err != nil {
				return uiSectionResult{}, err
			}
			section, err := s.savedSection(ctx, p.ProjectID)
			return uiSectionResult{Section: section}, err
		}),
	}
}

func (s *Server) setUIState(ctx context.Context, projectID string, state json.RawMessage) error {
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	previous, err := s.savedSection(ctx, projectID)
	if err != nil {
		return err
	}
	return s.writeUIState(ctx, projectID, previous, state)
}

func (s *Server) patchUIState(ctx context.Context, projectID, key string, value json.RawMessage) error {
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	saved, found, err := s.kv.Get(ctx, clientOwner, projectID)
	if err != nil {
		return err
	}
	state := map[string]json.RawMessage{}
	if found {
		if err := json.Unmarshal(saved, &state); err != nil || state == nil {
			state = map[string]json.RawMessage{}
		}
	}
	previous := sectionOf(saved)
	if trimmed := bytes.TrimSpace(value); len(trimmed) == 0 || string(trimmed) == "null" {
		delete(state, key)
	} else {
		state[key] = value
	}
	merged, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := validateUIState(merged); err != nil {
		return err
	}
	return s.writeUIState(ctx, projectID, previous, merged)
}

func (s *Server) writeUIState(ctx context.Context, projectID, previous string, state json.RawMessage) error {
	if err := s.kv.Set(ctx, clientOwner, projectID, state); err != nil {
		return err
	}
	if section := sectionOf(state); section != previous {
		s.publishSection(projectID, section)
	}
	return nil
}

func (s *Server) savedSection(ctx context.Context, projectID string) (string, error) {
	value, found, err := s.kv.Get(ctx, clientOwner, projectID)
	if err != nil || !found {
		return "", err
	}
	return sectionOf(value), nil
}

func sectionOf(state json.RawMessage) string {
	var v struct {
		Section string `json:"section"`
	}
	_ = json.Unmarshal(state, &v)
	return v.Section
}

func (s *Server) publishSection(projectID, section string) {
	if s.bus == nil {
		return
	}
	raw, err := json.Marshal(contract.UISection{ProjectID: projectID, Section: section})
	if err != nil {
		s.logger.Error("marshal section event", "err", err)
		return
	}
	if _, err := s.bus.Publish(context.Background(), contract.TopicUISection, coreSource, raw); err != nil {
		s.logger.Error("publish section event", "err", err)
	}
}

func (s *Server) checkUIStateClient(ctx context.Context, projectID string) error {
	if CallerFrom(ctx).IsPlugin() {
		return &jsonrpc2.Error{Code: codeInvalidParams, Message: "ui state requires a client caller"}
	}
	return s.checkUIStateProject(ctx, projectID)
}

func (s *Server) checkUIStateProject(ctx context.Context, projectID string) error {
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
