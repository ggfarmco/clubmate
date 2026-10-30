package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (a *app) registerProc(ctx context.Context, p *agentProc, workspace string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
	defer cancel()
	label := agentLabel(p.agent) + " · " + filepath.Base(p.root)
	if _, err := a.host.RegisterProcess(ctx, workspace, p.pgid, label); err != nil && p.alive() {
		fmt.Fprintln(os.Stderr, "agents: register agent process:", err)
	}
}

func (a *app) unregisterProc(p *agentProc) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := a.host.UnregisterProcess(ctx, p.pgid); err != nil && !p.quiet.Load() {
		fmt.Fprintln(os.Stderr, "agents: unregister agent process:", err)
	}
}
