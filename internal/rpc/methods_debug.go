package rpc

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"runtime/pprof"
	"time"
)

const (
	profileCPU        = "cpu"
	defaultCPUSeconds = 10
	maxCPUSeconds     = 120
)

type pprofParams struct {
	Profile string `json:"profile"`
	Debug   int    `json:"debug"`
	Seconds int    `json:"seconds"`
	GC      bool   `json:"gc"`
}

type pprofResult struct {
	Profile string `json:"profile"`
	Data    []byte `json:"data"`
}

func (s *Server) debugMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"debug.pprof": method(s.debugPprof),
	}
}

func (s *Server) debugPprof(ctx context.Context, p pprofParams) (pprofResult, error) {
	if CallerFrom(ctx).IsPlugin() {
		return pprofResult{}, invalidParams("debug.pprof is not available to plugins")
	}
	var buf bytes.Buffer
	if p.Profile == profileCPU {
		if err := cpuProfile(ctx, &buf, p.Seconds); err != nil {
			return pprofResult{}, err
		}
		return pprofResult{Profile: p.Profile, Data: buf.Bytes()}, nil
	}
	prof := pprof.Lookup(p.Profile)
	if prof == nil {
		return pprofResult{}, invalidParams(fmt.Sprintf("unknown profile %q", p.Profile))
	}
	if p.GC {
		runtime.GC()
	}
	if err := prof.WriteTo(&buf, p.Debug); err != nil {
		return pprofResult{}, fmt.Errorf("write %s profile: %w", p.Profile, err)
	}
	return pprofResult{Profile: p.Profile, Data: buf.Bytes()}, nil
}

func cpuProfile(ctx context.Context, buf *bytes.Buffer, seconds int) error {
	if seconds <= 0 {
		seconds = defaultCPUSeconds
	}
	if seconds > maxCPUSeconds {
		return invalidParams(fmt.Sprintf("seconds must be at most %d", maxCPUSeconds))
	}
	if err := pprof.StartCPUProfile(buf); err != nil {
		return fmt.Errorf("start cpu profile: %w", err)
	}
	t := time.NewTimer(time.Duration(seconds) * time.Second)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
	pprof.StopCPUProfile()
	return ctx.Err()
}
