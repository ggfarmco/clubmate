package rpc

import (
	"context"
	"time"
)

func SetBrowserGuard(s *Server, g func(context.Context, BrowserGuardInput) error) {
	s.browserGuard = g
}

func SetFSOverflow(s *Server, n int) {
	s.fsOverflow = n
}

func SetReportTimeout(s *Server, d time.Duration) {
	s.reportTimeout = d
}
