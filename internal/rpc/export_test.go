package rpc

import "context"

func SetBrowserGuard(s *Server, g func(context.Context, BrowserGuardInput) error) {
	s.browserGuard = g
}

func SetFSOverflow(s *Server, n int) {
	s.fsOverflow = n
}
