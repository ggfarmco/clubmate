package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"clubmate/pkg/pluginsdk"
)

var (
	errShutdown = errors.New("the agents plugin is shutting down")
)

type procSlot struct {
	ready chan struct{}
	proc  *agentProc
	err   error
}

func (a *app) ensureProc(ctx context.Context, agent, workspace, root string) (*agentProc, error) {
	k := agent + "\x00" + workspace + "\x00" + root
	for {
		a.mu.Lock()
		if a.closing {
			a.mu.Unlock()
			return nil, errShutdown
		}
		slot, ok := a.procs[k]
		if !ok {
			slot = &procSlot{ready: make(chan struct{})}
			a.procs[k] = slot
			a.mu.Unlock()
			p, err := a.startProc(ctx, agent, workspace, root)
			a.mu.Lock()
			slot.proc, slot.err = p, err
			if err != nil {
				delete(a.procs, k)
			}
			a.mu.Unlock()
			close(slot.ready)
			return p, err
		}
		a.mu.Unlock()
		select {
		case <-slot.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if slot.err != nil {
			return nil, slot.err
		}
		if slot.proc.alive() {
			return slot.proc, nil
		}
		a.mu.Lock()
		if a.procs[k] == slot {
			delete(a.procs, k)
		}
		a.mu.Unlock()
	}
}

func (a *app) startProc(ctx context.Context, agent, workspace, root string) (*agentProc, error) {
	l, err := launcherFor(context.WithoutCancel(ctx), a.stateDir, agent, workspace, root, a.setInstalling)
	if err != nil {
		return nil, err
	}
	p, err := startProc(l, agent, root, a.procExited)
	if err != nil {
		return nil, err
	}
	a.groups.add(p.pgid)
	go a.registerProc(ctx, p, workspace)
	a.mu.Lock()
	closing := a.closing
	if !closing {
		a.live[p] = true
	}
	a.mu.Unlock()
	if closing {
		p.shutdown()
		return nil, errShutdown
	}
	ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachTimeout)
	defer cancel()
	if err := p.initialize(ictx); err != nil {
		p.shutdown()
		if tail := p.stderr.tail(); tail != "" {
			err = fmt.Errorf("%w\n%s", err, tail)
		}
		return nil, err
	}
	a.uiMu.Lock()
	a.resumable[agent] = p.caps.SessionCapabilities.Resume != nil
	a.steers[agent] = p.steering
	a.uiMu.Unlock()
	return p, nil
}

func (a *app) registerProc(ctx context.Context, p *agentProc, workspace string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
	defer cancel()
	label := agentLabel(p.agent) + " · " + filepath.Base(p.root)
	if _, err := a.host.RegisterProcess(ctx, workspace, p.pgid, label); err != nil && p.alive() {
		fmt.Fprintln(os.Stderr, "agents: register agent process:", err)
	}
}

func (a *app) procExited(p *agentProc, reason string) {
	a.groups.remove(p.pgid)
	a.unregisterProc(p)
	a.mu.Lock()
	delete(a.live, p)
	a.dropSlotsLocked(p)
	a.mu.Unlock()
	for _, s := range p.attached() {
		s.mu.Lock()
		if s.proc != p {
			s.mu.Unlock()
			continue
		}
		s.proc = nil
		s.cancelPermissionsLocked()
		quiet := p.quiet.Load() || s.rec.Closed
		var ended turnPayload
		if !quiet {
			s.running = false
			s.log.flush()
			s.recordLocked(actorClubmate, kindError, turnEndPayload{Error: reason})
			s.setStatusLocked(statusError, reason)
			ended = s.turnLocked()
		}
		s.mu.Unlock()
		if !quiet {
			s.turned(ended)
			s.changed(changeStatus, true)
		}
	}
}

func (a *app) unregisterProc(p *agentProc) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := a.host.UnregisterProcess(ctx, p.pgid); err != nil && !p.quiet.Load() {
		fmt.Fprintln(os.Stderr, "agents: unregister agent process:", err)
	}
}

func (a *app) release(p *agentProc) {
	a.mu.Lock()
	if len(p.attached()) > 0 {
		a.mu.Unlock()
		return
	}
	a.dropSlotsLocked(p)
	a.mu.Unlock()
	go p.shutdown()
}

func (a *app) shutdown(context.Context, *pluginsdk.Host) {
	a.stopAll()
}

func (a *app) stopAll() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.closing = true
		procs := make([]*agentProc, 0, len(a.live))
		for p := range a.live {
			procs = append(procs, p)
		}
		sessions := make([]*session, 0, len(a.sessions))
		for _, s := range a.sessions {
			sessions = append(sessions, s)
		}
		a.mu.Unlock()
		a.uiMu.Lock()
		for _, p := range a.ui {
			p.tick.stop()
		}
		a.uiMu.Unlock()
		var wg sync.WaitGroup
		for _, p := range procs {
			wg.Go(p.shutdown)
		}
		wg.Wait()
		for _, s := range sessions {
			s.log.close()
		}
	})
}

func (a *app) dropSlotsLocked(p *agentProc) {
	maps.DeleteFunc(a.procs, func(_ string, slot *procSlot) bool { return slot.proc == p })
}
