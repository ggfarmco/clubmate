package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const (
	agentSessionPrefix = "agt_"
	callerCacheMax     = 1024
)

type callerWorkspaces struct {
	mu    sync.Mutex
	known map[string]string
}

func (c *callerWorkspaces) get(caller string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.known[caller]
}

func (c *callerWorkspaces) put(caller, workspaceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.known == nil || len(c.known) >= callerCacheMax {
		c.known = map[string]string{}
	}
	c.known[caller] = workspaceID
}

func (s *Server) requireCallerIn(ctx context.Context, caller, workspaceID string) error {
	if !strings.HasPrefix(caller, agentSessionPrefix) || workspaceID == "" || CallerFrom(ctx).IsPlugin() {
		return nil
	}
	if s.callers.get(caller) == workspaceID {
		return nil
	}
	u, err := s.uiState()
	if err != nil {
		return err
	}
	if _, ok := u.router.Resolve(cmdAgentsSessions); !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callerLookup)
	defer cancel()
	raw, err := s.runPluginCommand(ctx, cmdAgentsSessions, workspaceID, map[string]string{"session": caller})
	if err != nil {
		return fmt.Errorf("cannot check the calling session %s: %w", caller, err)
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("cannot check the calling session %s: %w", caller, err)
	}
	for _, sess := range list {
		if sess.ID == caller {
			s.callers.put(caller, workspaceID)
			return nil
		}
	}
	return fmt.Errorf("%w: the calling session %s is not in workspace %q; agents act only in their own workspace", ErrDenied, caller, workspaceID)
}
