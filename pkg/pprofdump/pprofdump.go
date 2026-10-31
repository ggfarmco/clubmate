package pprofdump

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/pprof"
	"time"
)

const (
	Method            = "debug.pprof"
	ProfileCPU        = "cpu"
	defaultCPUSeconds = 10
	MaxCPUSeconds     = 120
)

var ErrInvalid = errors.New("invalid profile request")

type Params struct {
	Profile string `json:"profile"`
	Debug   int    `json:"debug"`
	Seconds int    `json:"seconds"`
	GC      bool   `json:"gc"`
}

type Result struct {
	Profile string `json:"profile"`
	Data    []byte `json:"data"`
}

func Write(ctx context.Context, p Params) (Result, error) {
	var buf bytes.Buffer
	if p.Profile == ProfileCPU {
		if err := cpuProfile(ctx, &buf, p.Seconds); err != nil {
			return Result{}, err
		}
		return Result{Profile: p.Profile, Data: buf.Bytes()}, nil
	}
	prof := pprof.Lookup(p.Profile)
	if prof == nil {
		return Result{}, fmt.Errorf("%w: unknown profile %q", ErrInvalid, p.Profile)
	}
	if p.GC {
		runtime.GC()
	}
	if err := prof.WriteTo(&buf, p.Debug); err != nil {
		return Result{}, fmt.Errorf("write %s profile: %w", p.Profile, err)
	}
	return Result{Profile: p.Profile, Data: buf.Bytes()}, nil
}

func (p Params) Duration() time.Duration {
	if p.Profile != ProfileCPU {
		return 0
	}
	return time.Duration(cpuSeconds(p.Seconds)) * time.Second
}

func cpuProfile(ctx context.Context, buf *bytes.Buffer, seconds int) error {
	if seconds > MaxCPUSeconds {
		return fmt.Errorf("%w: seconds must be at most %d", ErrInvalid, MaxCPUSeconds)
	}
	if err := pprof.StartCPUProfile(buf); err != nil {
		return fmt.Errorf("start cpu profile: %w", err)
	}
	t := time.NewTimer(time.Duration(cpuSeconds(seconds)) * time.Second)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
	pprof.StopCPUProfile()
	return ctx.Err()
}

func cpuSeconds(seconds int) int {
	if seconds <= 0 {
		return defaultCPUSeconds
	}
	return seconds
}
