package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

const (
	handleUISnapshot = "ui.snapshot"
	handleUIPresent  = "ui.present"
	handleUIKey      = "ui.key"
	testCallTimeout  = 30 * time.Second
)

func (s *Server) testModeMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		handleUISnapshot: method(s.forwardToTestApp(handleUISnapshot, "snapshot")),
		handleUIPresent:  method(s.forwardToTestApp(handleUIPresent, "present")),
		handleUIKey:      method(s.forwardToTestApp(handleUIKey, "key input")),
	}
}

func (s *Server) forwardToTestApp(handle, what string) func(context.Context, json.RawMessage) (json.RawMessage, error) {
	return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		conn := s.testClient(handle)
		if conn == nil {
			return nil, &jsonrpc2.Error{Code: codeNotFound, Message: fmt.Sprintf("%s is only available in test mode", what)}
		}
		ctx, cancel := context.WithTimeout(ctx, testCallTimeout)
		defer cancel()
		var res json.RawMessage
		if err := conn.Call(ctx, handle, params, &res); err != nil {
			return nil, err
		}
		return res, nil
	}
}

func (s *Server) testClient(handle string) *jsonrpc2.Conn {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	var (
		best *jsonrpc2.Conn
		seq  uint64
	)
	for conn, c := range s.clients {
		if _, ok := c.handles[handle]; ok && (best == nil || c.seq > seq) {
			best, seq = conn, c.seq
		}
	}
	return best
}
