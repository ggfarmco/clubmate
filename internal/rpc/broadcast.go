package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
)

type broadcastParams struct {
	Topic     string          `json:"topic"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args"`
}

type refusalError struct {
	reasons []string
}

func (s *Server) commandBroadcast(ctx context.Context, p broadcastParams) (empty, error) {
	u, err := s.uiState()
	if err != nil {
		return empty{}, err
	}
	if p.Topic == "" {
		return empty{}, invalidParams("topic is required")
	}
	if err := s.requireProject(ctx, p.ProjectID); err != nil {
		return empty{}, err
	}
	var handlers []Command
	for _, c := range u.router.Commands() {
		if slices.Contains(c.Handles, p.Topic) {
			handlers = append(handlers, c)
		}
	}
	via := CallerFrom(ctx).PluginID
	errs := make([]error, len(handlers))
	var wg sync.WaitGroup
	for i, c := range handlers {
		wg.Go(func() {
			raw, err := json.Marshal(commandRunParams{ID: c.ID, ProjectID: p.ProjectID, Args: p.Args, Via: via})
			if err == nil {
				_, err = u.router.Command(ctx, c, raw)
			}
			errs[i] = err
		})
	}
	wg.Wait()
	return empty{}, refusal(errs)
}

func (e *refusalError) Error() string {
	return strings.Join(e.reasons, "; ")
}

func (e *refusalError) Is(target error) bool {
	return target == ErrPluginError
}

func refusal(errs []error) error {
	var reasons []string
	for _, err := range errs {
		if err == nil {
			continue
		}
		if pe, ok := errors.AsType[*PluginError](err); ok {
			reasons = append(reasons, pe.Message)
			continue
		}
		reasons = append(reasons, err.Error())
	}
	if len(reasons) == 0 {
		return nil
	}
	return &refusalError{reasons: reasons}
}
