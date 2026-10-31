package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	callTimeout     = 10 * time.Second
	attachTimeout   = 2 * time.Minute
	refsTimeout     = 30 * time.Second
	checkpointLimit = 2 * time.Minute
)

var (
	errNoStateDir = errors.New("no plugin state dir")
	errNoSession  = errors.New("unknown agent session")
	errClosed     = errors.New("the session is closed")
)

type app struct {
	host      *pluginsdk.Host
	stateDir  string
	loaded    chan struct{}
	quit      chan struct{}
	notes     *notifier
	groups    *groups
	codePanel bool

	spawnMu    sync.Mutex
	worktreeMu sync.Mutex
	stopOnce   sync.Once

	registry
	uiState
	waiters
}

type registry struct {
	mu       sync.Mutex
	sessions map[string]*session
	procs    map[string]*procSlot
	live     map[*agentProc]bool
	dropped  map[string]dropState
	closing  bool
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type sectionRef struct {
	ID string `json:"id"`
}

func main() {
	a := &app{loaded: make(chan struct{}), quit: make(chan struct{}), registry: newRegistry(), uiState: newUIState()}
	signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM)
	err := pluginsdk.Run(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, Shutdown: a.shutdown})
	a.stopAll()
	if err != nil {
		os.Exit(1)
	}
}

func newRegistry() registry {
	return registry{sessions: map[string]*session{}, procs: map[string]*procSlot{}, live: map[*agentProc]bool{}, dropped: map[string]dropState{}}
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.host = h
	a.stateDir = pluginsdk.StateDir()
	if a.stateDir == "" {
		return errNoStateDir
	}
	a.notes = newNotifier(h)
	a.groups = newGroups(a.stateDir)
	a.groups.reap(orphanMarkers())
	var sections []sectionRef
	if err := h.Call(ctx, "ui.sections", nil, &sections); err != nil {
		return err
	}
	a.codePanel = slices.ContainsFunc(sections, func(s sectionRef) bool { return s.ID == editorSection })
	keys, err := h.StorageList(ctx, sessionKey)
	if err != nil {
		return err
	}
	for _, k := range keys {
		var rec record
		if found, err := h.StorageGet(ctx, k, &rec); err != nil || !found || !api.Valid(rec.ID) {
			continue
		}
		stale := rec.Status == statusRunning || rec.Status == statusWaiting
		if stale {
			rec.Status = statusIdle
		}
		s := a.addSession(rec, false, nil)
		if stale {
			a.notes.mark(s, changeStatus, true)
		}
	}
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "project.*", ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "session.exited", ready.Seq); err != nil {
		return err
	}
	close(a.loaded)
	go a.suspendIdleLoop(suspendDelay())
	a.renderAll(ctx)
	return nil
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "session.exited":
		a.signInExited(ctx, ev)
	case "project.created":
		var p projectPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.kick(p.Project.ID)
		}
	case "project.deleted":
		var p deletedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.forgetProject(p.ID)
			go a.dropWorkspace(p.ID, nil, true)
		}
	case "project.updated":
		var p projectPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.kick(p.Project.ID)
			go a.dropWorkspace(p.Project.ID, func(rec record) bool {
				_, err := os.Stat(rec.Worktree)
				return errors.Is(err, os.ErrNotExist)
			}, false)
		}
	}
}

func (a *app) addSession(rec record, loaded bool, ready *setup) *session {
	var s *session
	s = newSession(rec, newEventLog(logPath(a.stateDir, rec.ID)), loaded, func(kind string, save bool) {
		if kind == changeStatus {
			a.wakeWaiters()
		}
		a.notes.mark(s, kind, save)
		a.kick(rec.Workspace)
	})
	s.setup = ready
	s.turned = a.notes.turn
	a.mu.Lock()
	a.sessions[rec.ID] = s
	a.mu.Unlock()
	return s
}

func (a *app) session(id string) *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[id]
}

func (a *app) lookup(projectID, id string) (*session, error) {
	a.mu.Lock()
	s, ok := a.sessions[id]
	a.mu.Unlock()
	if !ok || s.snapshot().Workspace != projectID {
		return nil, fmt.Errorf("%w: %s", errNoSession, id)
	}
	return s, nil
}

func (a *app) list(projectID string) []record {
	a.mu.Lock()
	all := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		all = append(all, s)
	}
	a.mu.Unlock()
	out := []record{}
	for _, s := range all {
		if rec := s.snapshot(); rec.Workspace == projectID {
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(x, y record) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return out
}

func (a *app) infos(projectID string) []sessionInfo {
	recs := a.list(projectID)
	out := make([]sessionInfo, 0, len(recs))
	for _, rec := range recs {
		info := sessionInfo{record: rec, NotifyEffective: a.effectiveNotify(rec)}
		if s := a.session(rec.ID); s != nil {
			info.Pending = s.pending()
			info.Asks = s.asks()
		}
		out = append(out, info)
	}
	return out
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}
