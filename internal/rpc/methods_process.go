package rpc

import (
	"context"

	"clubmate/internal/core"
	"clubmate/internal/id"
	"clubmate/internal/session"
)

type processRegisterParams struct {
	ProjectID string `json:"projectId"`
	PID       int    `json:"pid"`
	Label     string `json:"label"`
}

type processUnregisterParams struct {
	PID int `json:"pid"`
}

type processRootsParams struct {
	ProjectID string `json:"projectId"`
}

func (s *Server) processMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"process.register": method(func(ctx context.Context, p processRegisterParams) (core.ProcessRoot, error) {
			owner, err := pluginOwner(ctx, "process.register")
			if err != nil {
				return core.ProcessRoot{}, err
			}
			if err := requireID(projectPrefix, p.ProjectID); err != nil {
				return core.ProcessRoot{}, err
			}
			return s.svc.RegisterProcess(ctx, owner, core.RegisterRequest(p))
		}),
		"process.unregister": method(func(ctx context.Context, p processUnregisterParams) (empty, error) {
			owner, err := pluginOwner(ctx, "process.unregister")
			if err != nil {
				return empty{}, err
			}
			s.svc.UnregisterProcess(owner, p.PID)
			return empty{}, nil
		}),
		"process.roots": method(func(ctx context.Context, p processRootsParams) ([]core.ProcessRoot, error) {
			if err := optionalID(projectPrefix, p.ProjectID); err != nil {
				return nil, err
			}
			return nonNil(s.svc.ProcessRoots(p.ProjectID)), nil
		}),
	}
}

func pluginOwner(ctx context.Context, name string) (core.RootOwner, error) {
	caller := CallerFrom(ctx)
	if !caller.IsPlugin() || caller.PID <= 1 {
		return core.RootOwner{}, invalidParams(name + " requires a plugin caller")
	}
	return caller.rootOwner(), nil
}

func (c Caller) rootOwner() core.RootOwner {
	return core.RootOwner{Plugin: c.PluginID, PID: c.PID}
}

func optionalGroupID(s string) error {
	if s == "" || id.Valid(core.RootIDPrefix, s) {
		return nil
	}
	return requireID(session.IDPrefix, s)
}
