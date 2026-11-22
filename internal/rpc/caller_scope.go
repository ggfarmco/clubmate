package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"clubmate/internal/workspace"
)

const (
	agentSessionPrefix = "agt_"
	callerCacheMax     = 1024
	homeReadsOnly      = "from Home only read tools reach other workspaces"
)

type listedSession struct {
	ID string `json:"id"`
}

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

func (s *Server) requireCallerIn(ctx context.Context, caller, workspaceID string, reading bool) error {
	if !agentCaller(ctx, caller) || workspaceID == "" {
		return nil
	}
	known, err := s.callersKnown()
	if err != nil || !known {
		return err
	}
	home, err := s.cachedInHome(ctx, caller, workspaceID)
	if err != nil {
		return err
	}
	if !home {
		listed, err := s.callerListed(ctx, caller, workspaceID)
		if err != nil || listed {
			return err
		}
		if home, err = s.callerInHome(ctx, caller); err != nil {
			return err
		}
	}
	switch {
	case home && reading:
		return nil
	case home:
		return fmt.Errorf("%w: the calling session %s is not in workspace %q; %s", ErrDenied, caller, workspaceID, homeReadsOnly)
	}
	return fmt.Errorf("%w: the calling session %s is not in workspace %q; agents act only in their own workspace", ErrDenied, caller, workspaceID)
}

func (s *Server) requireScopeIn(ctx context.Context, c Command, caller, workspaceID string) error {
	home, err := s.inHome(ctx, caller, workspaceID)
	switch {
	case err != nil:
		return err
	case home && c.ReadOnly:
		return nil
	case home:
		return fmt.Errorf("%w: the session is not in workspace %q; %s", ErrDenied, workspaceID, homeReadsOnly)
	}
	return fmt.Errorf("%w: the session is not in workspace %q; agents act only in their own workspace", ErrDenied, workspaceID)
}

func (s *Server) inHome(ctx context.Context, caller, workspaceID string) (bool, error) {
	switch {
	case workspaceID == "":
		return false, nil
	case agentCaller(ctx, caller):
		return s.callerInHome(ctx, caller)
	}
	return s.isHome(ctx, workspaceID)
}

func (s *Server) cachedInHome(ctx context.Context, caller, workspaceID string) (bool, error) {
	id := s.callers.get(caller)
	if id == "" || id == workspaceID {
		return false, nil
	}
	return s.isHome(ctx, id)
}

func (s *Server) isHome(ctx context.Context, id string) (bool, error) {
	ws, err := s.svc.GetWorkspace(ctx, id)
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return ws.Home, nil
}

func (s *Server) callerInHome(ctx context.Context, caller string) (bool, error) {
	if id := s.callers.get(caller); id != "" {
		return s.isHome(ctx, id)
	}
	known, err := s.callersKnown()
	if err != nil || !known {
		return false, err
	}
	workspaces, err := s.svc.ListWorkspaces(ctx)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(workspaces, func(ws workspace.Workspace) bool { return ws.Home })
	if i < 0 {
		return false, nil
	}
	return s.callerListed(ctx, caller, workspaces[i].ID)
}

func agentCaller(ctx context.Context, caller string) bool {
	return strings.HasPrefix(caller, agentSessionPrefix) && !CallerFrom(ctx).IsPlugin()
}

func (s *Server) callersKnown() (bool, error) {
	u, err := s.uiState()
	if err != nil {
		return false, err
	}
	_, ok := u.router.Resolve(cmdAgentsSessions)
	return ok, nil
}

func (s *Server) callerListed(ctx context.Context, caller, workspaceID string) (bool, error) {
	if s.callers.get(caller) == workspaceID {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, callerLookup)
	defer cancel()
	raw, err := s.runPluginCommand(ctx, cmdAgentsSessions, workspaceID, map[string]string{"session": caller})
	if err != nil {
		return false, fmt.Errorf("cannot check the calling session %s: %w", caller, err)
	}
	var list []listedSession
	if err := json.Unmarshal(raw, &list); err != nil {
		return false, fmt.Errorf("cannot check the calling session %s: %w", caller, err)
	}
	if !slices.ContainsFunc(list, func(sess listedSession) bool { return sess.ID == caller }) {
		return false, nil
	}
	s.callers.put(caller, workspaceID)
	return true, nil
}
