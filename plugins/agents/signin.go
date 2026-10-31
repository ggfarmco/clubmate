package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	signInTitle = "Sign in to "
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
	if rpcErr, ok := errors.AsType[*acp.Error](err); !ok || rpcErr.Code != acp.CodeAuthRequired {
		return nil
	}
	return a.needsAuth(sid, p)
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

func (a *app) signIn(ctx context.Context, projectID string, s *session, st *sessionUI) error {
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
		ProjectID: projectID,
		Title:     signInTitle + agentLabel(rec.Agent),
		Command:   cmd.argv,
		Cwd:       rec.Worktree,
		Env:       cmd.env,
		UnsetEnv:  []string{apiKeyEnv},
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
		for _, proj := range a.ui {
			if st := proj.sessions[sid]; st != nil && st.signIn == p.ID {
				st.signIn = ""
			}
		}
	}
	a.uiMu.Unlock()
	if !ok {
		return
	}
	a.removeTerminal(ctx, p.ID, false)
	if s := a.session(sid); s != nil {
		a.resume(s)
	}
}

func (a *app) cancelSignIn(ctx context.Context, projectID, sid string) {
	a.uiMu.Lock()
	if p := a.ui[projectID]; p != nil {
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

func (a *app) removeLeftoverTerminals(ctx context.Context, projects []pluginsdk.Project) {
	for _, p := range projects {
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
