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

	"clubmate/pkg/contract"
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
	errNoTurn     = errors.New("the session has no such turn")
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
	usage     usageTracker
	branches  branchCache
	texts     textIndex
	watchdog  watchdog
	reviews   *reviewBoard

	sessionRevs sessionRevs
	runNeeds    runNeeds

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

type sectionRef struct {
	ID string `json:"id"`
}

func main() {
	a := &app{loaded: make(chan struct{}), quit: make(chan struct{}), watchdog: newWatchdog(), reviews: newReviewBoard(), registry: newRegistry(), uiState: newUIState()}
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
	if err := a.usage.load(ctx, h); err != nil {
		return err
	}
	a.codePanel = slices.ContainsFunc(sections, func(s sectionRef) bool { return s.ID == editorSection })
	recs, err := loadRecords(ctx, h)
	if err != nil {
		return err
	}
	var cut []*session
	for _, rec := range recs {
		stale := rec.Status == statusRunning || rec.Status == statusWaiting
		if stale {
			rec.Status = statusIdle
		}
		s := a.addSession(rec, false, nil)
		if stale {
			a.notes.mark(s, changeStatus, true)
			cut = append(cut, s)
		}
	}
	go a.texts.warm(a.stateDir, recs)
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "workspace.*", ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "session.exited", ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, "ui.*", ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, gitChangedTopic, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, editorFileTopic, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, reviewTopic, ready.Seq); err != nil {
		return err
	}
	for _, topic := range []string{docsOpenedTopic, docsPresentedTopic} {
		if _, err := h.Subscribe(ctx, topic, ready.Seq); err != nil {
			return err
		}
	}
	watched, err := h.WatchedSections(ctx, ready.Seq)
	if err != nil {
		return err
	}
	for id, sections := range watched {
		a.setWatched(id, sections)
	}
	if _, err := h.Subscribe(ctx, contract.TopicRemoteWatching, ready.Seq); err != nil {
		return err
	}
	if err := a.loadPresence(ctx); err != nil {
		return err
	}
	close(a.loaded)
	if err := a.renderSettings(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render settings:", err)
	}
	go a.suspendIdleLoop(envDelay(suspendAfterEnv, suspendAfter))
	go a.closeDoneLoop(envDelay(closeAfterEnv, closeAfter))
	go a.continueCutOff(cut)
	a.renderAll(ctx)
	return nil
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case "session.exited":
		a.signInExited(ctx, ev)
	case "ui.opened", "ui.idle", "ui.section":
		a.presenceEvent(ctx, ev)
	case gitChangedTopic:
		var p gitChanged
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.branches.reset()
			a.kick(p.Workspace)
		}
	case editorFileTopic:
		var f editorFile
		if json.Unmarshal(ev.Payload, &f) == nil && a.setEditorFile(f) {
			a.kick(f.Workspace)
		}
	case reviewTopic:
		if ws, people, ok := a.reviews.apply(ev.Payload); ok {
			for _, id := range people {
				if s := a.session(id); s != nil {
					a.notes.mark(s, changeReview, false)
				}
			}
			a.kick(ws)
		}
	case docsOpenedTopic, docsPresentedTopic:
		var p docsPresented
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.docPresented(p)
		}
	case contract.TopicRemoteWatching:
		var p contract.RemoteWatching
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.setWatched(p.Workspace, p.Sections)
			a.kick(p.Workspace)
		}
	case contract.TopicWorkspaceCreated:
		var p contract.WorkspaceChanged
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.kick(p.Workspace)
		}
	case contract.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.forgetWorkspace(p.Workspace)
			go a.dropWorkspace(p.Workspace, nil, true)
		}
	case contract.TopicWorkspaceUpdated:
		var p contract.WorkspaceChanged
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.kick(p.Workspace)
			go a.dropWorkspace(p.Workspace, func(rec record) bool {
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
			a.statusChanged(s)
		}
		a.notes.mark(s, kind, save)
		a.kick(rec.Workspace)
	})
	s.setup = ready
	s.turned = func(t api.Turn) {
		t.Finished = a.finished(t)
		a.notes.turn(t)
	}
	s.idled = func() { a.settleBackground(s) }
	s.resolved = func(kind, request string) { a.notes.resolved(s, kind, request) }
	s.requested = func(kind, request string) {
		if kind != entryElicitation || !a.asksParent(s, s.snapshot()) {
			a.notes.requested(s, kind, request)
		}
	}
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

func (a *app) lookup(workspaceID, id string) (*session, error) {
	a.mu.Lock()
	s, ok := a.sessions[id]
	a.mu.Unlock()
	if !ok || s.snapshot().Workspace != workspaceID {
		return nil, fmt.Errorf("%w: %s", errNoSession, id)
	}
	return s, nil
}

func (a *app) list(workspaceID string) []record {
	a.mu.Lock()
	all := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		all = append(all, s)
	}
	a.mu.Unlock()
	out := []record{}
	for _, s := range all {
		if rec := s.snapshot(); rec.Workspace == workspaceID {
			rec.AsksParent = a.asksParent(s, rec)
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(x, y record) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return out
}

func (a *app) infos(workspaceID string) []sessionInfo {
	recs := a.list(workspaceID)
	children := childStatuses(recs)
	out := make([]sessionInfo, 0, len(recs))
	for _, rec := range recs {
		info := sessionInfo{record: rec, NotifyEffective: a.effectiveNotify(rec), ChildStatus: children[rec.ID]}
		if s := a.session(rec.ID); s != nil {
			info.Pending = s.pending()
			info.Asks = s.asks()
			info.PendingSummary = s.permissionSummary()
			info.Activity = s.toolActivity()
			info.ModeLabel = s.modeLabel()
			if started := s.turnStart().UTC(); rec.Status == statusRunning && !started.IsZero() {
				info.TurnStartedAt = &started
			}
		}
		if info.ModeLabel == "" {
			info.ModeLabel = modeWord(rec.Mode, nil)
		}
		info.ReviewProgress = a.reviews.progress(rec.ID)
		info.NeedsYou = needsYou(info.record)
		info.Service = rec.restricted()
		if rec.Status == statusError {
			failure := failureOf(rec)
			a.uiMu.Lock()
			needsAuth, cannotResume := a.recoveryLocked(rec)
			a.uiMu.Unlock()
			failure.Actions = failureActions(rec.ID, needsAuth, cannotResume)
			info.Failure = &failure
		}
		if info.PendingSummary == nil && info.Asks && rec.Question != "" {
			info.PendingSummary = &pendingSummary{Kind: pendingQuestion, Text: rec.Question, At: rec.UpdatedAt}
		}
		if info.NeedsYou {
			since := info.UpdatedAt
			if info.PendingSummary != nil {
				since = info.PendingSummary.At
			}
			info.NeedsYouSince = &since
		}
		out = append(out, info)
	}
	for _, n := range a.runNeeds.list(workspaceID) {
		out = append(out, runInfo(workspaceID, n))
	}
	setGroups(out)
	return out
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}
