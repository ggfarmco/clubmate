package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"clubmate/pkg/pprofdump"
)

const pluginPprofSlack = 30 * time.Second

type pprofParams struct {
	pprofdump.Params
	Plugin string `json:"plugin"`
}

func (s *Server) debugMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		pprofdump.Method: method(s.debugPprof),
	}
}

func (s *Server) debugPprof(ctx context.Context, p pprofParams) (pprofdump.Result, error) {
	if CallerFrom(ctx).IsPlugin() {
		return pprofdump.Result{}, invalidParams("debug.pprof is not available to plugins")
	}
	if p.Plugin != "" {
		return s.pluginPprof(ctx, p)
	}
	res, err := pprofdump.Write(ctx, p.Params)
	if errors.Is(err, pprofdump.ErrInvalid) {
		return pprofdump.Result{}, invalidParams(err.Error())
	}
	return res, err
}

func (s *Server) pluginPprof(ctx context.Context, p pprofParams) (pprofdump.Result, error) {
	if p.Seconds > pprofdump.MaxCPUSeconds {
		return pprofdump.Result{}, invalidParams(fmt.Sprintf("seconds must be at most %d", pprofdump.MaxCPUSeconds))
	}
	u, err := s.uiState()
	if err != nil {
		return pprofdump.Result{}, err
	}
	raw, err := json.Marshal(p.Params)
	if err != nil {
		return pprofdump.Result{}, err
	}
	out, err := u.router.Pprof(ctx, p.Plugin, raw, p.Duration()+pluginPprofSlack)
	if err != nil {
		return pprofdump.Result{}, err
	}
	var res pprofdump.Result
	if err := json.Unmarshal(out, &res); err != nil {
		return pprofdump.Result{}, fmt.Errorf("decode %s profile: %w", p.Plugin, err)
	}
	return res, nil
}
