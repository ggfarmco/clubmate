package rpc

import "context"

func SetBrowserGuard(s *Server, g func(context.Context, BrowserGuardInput) error) {
	s.browserGuard = g
}
