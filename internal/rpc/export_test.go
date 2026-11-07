package rpc

import (
	"context"
	"time"
)

func SetBrowserGuard(s *Server, g func(context.Context, BrowserGuardInput) error) {
	s.browserGuard = g
}

func BrowserURLCount(s *Server) int {
	s.browserURLsMu.Lock()
	defer s.browserURLsMu.Unlock()
	return len(s.browserURLs)
}

func SetFSOverflow(s *Server, n int) {
	s.fsOverflow = n
}

func SetReportTimeout(s *Server, d time.Duration) {
	s.reportTimeout = d
}
