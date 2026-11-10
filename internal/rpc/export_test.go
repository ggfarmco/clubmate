package rpc

import (
	"context"
	"net/netip"
	"time"
)

func SetBrowserGuard(s *Server, g func(context.Context, BrowserGuardInput) error) {
	s.browserGuard = g
}

func SetBrowserResolver(s *Server, r func(context.Context, string) ([]netip.Addr, error)) {
	s.browserResolve = r
}

func CheckBrowserURL(ctx context.Context, r func(context.Context, string) ([]netip.Addr, error), raw string) error {
	return checkBrowserURL(ctx, r, raw)
}

func SetFSOverflow(s *Server, n int) {
	s.fsOverflow = n
}

func SetReportTimeout(s *Server, d time.Duration) {
	s.reportTimeout = d
}
