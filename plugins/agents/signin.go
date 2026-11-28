package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	signInTitle    = "Sign in to "
	signedOutStart = "Failed to authenticate. API Error: 401"
)

var (
	errSignIn   = errors.New("sign in to continue")
	errNoSignIn = errors.New("the agent did not ask to sign in")
)

type authCommand struct {
	argv  []string
	env   []string
	label string
}

type ptyEvent struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
}

func (a *app) authRequired(sid string, p *agentProc, err error) error {
	if !needsSignIn(err) {
		return nil
	}
	return a.needsAuth(sid, p)
}

func isAuthRequired(err error) bool {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	return ok && rpcErr.Code == acp.CodeAuthRequired
}

func needsSignIn(err error) bool {
	f, ok := classifyFailure(err)
	return isAuthRequired(err) || ok && f.kind == failureAuth
}

func signedOutText(kind string, payload json.RawMessage) string {
	var chunk acp.ContentChunk
	if kind != acp.UpdateAgentMessageChunk || json.Unmarshal(payload, &chunk) != nil || chunk.Content.Type != "text" {
		return ""
	}
	text := strings.TrimSpace(chunk.Content.Text)
	if !strings.HasPrefix(text, signedOutStart) || strings.Contains(text, "\n") {
		return ""
	}
	return text
}

func (a *app) cycleSignedOut(s *session) bool {
	s.mu.Lock()
	text, p, id := s.cycleAuth, s.proc, s.rec.ID
	stale := s.workingLocked() || s.rec.closed() || s.deleted
	if !stale {
		s.cycleAuth = ""
	}
	s.mu.Unlock()
	if text == "" || stale {
		return false
	}
	status := failurePolicies[failureAuth].title
	if p != nil {
		if err := a.needsAuth(id, p); err != nil {
			status = err.Error()
		}
	}
	s.mu.Lock()
	if s.workingLocked() || s.rec.closed() || s.deleted {
		s.mu.Unlock()
		return false
	}
	s.ending = nil
	s.recordLocked(actorClubmate, kindTurnEnd, turnEndPayload{Error: text})
	s.setStatusLocked(statusError, status)
	s.rec.AuthWait = s.rec.Night == ""
	ended := s.turnLocked()
	s.mu.Unlock()
	s.turned(ended)
	s.changed(changeStatus, true)
	a.notifyEnd(s, ended)
	return true
}

func (a *app) continueSignedIn() map[string]bool {
	resumed := map[string]bool{}
	for _, s := range a.allSessions() {
		s.mu.Lock()
		wait := s.rec.AuthWait && !s.running
		due := wait && s.rec.Status == statusError && !s.rec.closed() && !s.deleted
		if wait {
			s.rec.AuthWait = false
		}
		id := s.rec.ID
		s.mu.Unlock()
		if !wait {
			continue
		}
		s.changed(changeStatus, true)
		if !due {
			continue
		}
		a.uiMu.Lock()
		delete(a.auth, id)
		a.uiMu.Unlock()
		a.resume(s)
		resumed[id] = true
	}
	return resumed
}

func (a *app) needsAuth(sid string, p *agentProc) error {
	cmd, ok := p.terminalAuth()
	if !ok {
		return nil
	}
	a.uiMu.Lock()
	a.auth[sid] = cmd
	a.uiMu.Unlock()
	return fmt.Errorf("%w: %s", errSignIn, cmp.Or(cmd.label, agentLabel(p.agent)))
}

func (a *app) signIn(ctx context.Context, workspaceID string, s *session, st *sessionUI) error {
	rec := s.snapshot()
	a.uiMu.Lock()
	cmd, ok := a.auth[rec.ID]
	running := st.signIn
	a.uiMu.Unlock()
	if !ok {
		return errNoSignIn
	}
	if running != "" {
		return nil
	}
	pty, err := a.host.CreateSession(ctx, pluginsdk.SessionRequest{
		WorkspaceID: workspaceID,
		Title:       signInTitle + agentLabel(rec.Agent),
		Command:     cmd.argv,
		Cwd:         rec.Worktree,
		Env:         cmd.env,
		UnsetEnv:    []string{apiKeyEnv},
	})
	if err != nil {
		return err
	}
	a.uiMu.Lock()
	st.signIn = pty.ID
	a.signIns[pty.ID] = rec.ID
	a.uiMu.Unlock()
	return nil
}

func (a *app) signInExited(ctx context.Context, ev pluginsdk.Event) {
	var p ptyEvent
	if json.Unmarshal(ev.Payload, &p) != nil || p.Owner != a.host.PluginID() {
		return
	}
	a.uiMu.Lock()
	sid, ok := a.signIns[p.ID]
	delete(a.signIns, p.ID)
	if ok {
		delete(a.auth, sid)
		for _, ws := range a.ui {
			if st := ws.sessions[sid]; st != nil && st.signIn == p.ID {
				st.signIn = ""
			}
		}
	}
	a.uiMu.Unlock()
	if !ok {
		return
	}
	a.removeTerminal(ctx, p.ID, false)
	a.recheckSetup()
	resumed := a.continueSignedIn()
	if s := a.session(sid); s != nil && !resumed[sid] {
		a.resume(s)
	}
}

func (a *app) cancelSignIn(ctx context.Context, workspaceID, sid string) {
	a.uiMu.Lock()
	if p := a.ui[workspaceID]; p != nil {
		if st := p.sessions[sid]; st != nil {
			st.signIn = ""
		}
	}
	delete(a.auth, sid)
	var ptys []string
	for pty, owner := range a.signIns {
		if owner == sid {
			ptys = append(ptys, pty)
			delete(a.signIns, pty)
		}
	}
	a.uiMu.Unlock()
	for _, pty := range ptys {
		a.removeTerminal(ctx, pty, true)
	}
}

func (a *app) removeLeftoverTerminals(ctx context.Context, workspaces []pluginsdk.Workspace) {
	for _, p := range workspaces {
		sessions, err := a.host.ListSessions(ctx, p.ID, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: list sign-in terminals:", err)
			continue
		}
		for _, s := range sessions {
			a.removeTerminal(ctx, s.ID, s.State == "running")
		}
	}
}

func (a *app) removeTerminal(ctx context.Context, id string, running bool) {
	if running {
		if err := a.host.KillSession(ctx, id); err != nil {
			fmt.Fprintln(os.Stderr, "agents: kill sign-in terminal:", err)
		}
	}
	if err := a.host.RemoveSession(ctx, id); err != nil {
		fmt.Fprintln(os.Stderr, "agents: remove sign-in terminal:", err)
	}
}
