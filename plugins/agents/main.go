package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/delta"
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
	sendHold  time.Duration
	loaded    chan struct{}
	quit      chan struct{}
	notes     *notifier
	groups    *groups
	codePanel bool
	usage     usageTracker
	branches  branchCache
	texts     textIndex
	found     searchCache
	watchdog  watchdog
	reviews   *reviewBoard
	self      *selfStore

	sessionRevs delta.Observed
	runNeeds    runNeeds
	reminders   reminders
	openWake    chan struct{}

	spawnMu    sync.Mutex
	leaving    map[string]int
	worktreeMu sync.Mutex
	connectMu  sync.Mutex
	cloners    map[string]bool
	landMu     sync.Mutex
	gateMu     sync.Mutex
	gateLogMu  sync.Mutex
	liveGates  atomic.Pointer[map[string]bool]
	gateRepos  sync.Map
	landWork   coalesced
	onceLocks  onceLocks
	archiving  archiveClaims
	declareMu  sync.Mutex
	nightMu    sync.Mutex
	stopOnce   sync.Once

	nightStopMu sync.Mutex
	nightStop   context.CancelCauseFunc

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
	a := &app{loaded: make(chan struct{}), quit: make(chan struct{}), openWake: make(chan struct{}, 1), watchdog: newWatchdog(), reviews: newReviewBoard(), registry: newRegistry(), uiState: newUIState()}
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
	a.sessionRevs.Store = h
	a.stateDir = pluginsdk.StateDir()
	if a.stateDir == "" {
		return errNoStateDir
	}
	a.notes = newNotifier(h, a.stateDir)
	a.sendHold = envDelay(sendHoldEnv, 0)
	a.groups = newGroups(a.stateDir)
	a.groups.reap(orphanMarkers())
	a.self = newSelfStore(a.stateDir)
	installHooks(a.stateDir)
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
	cut := map[*session]string{}
	for _, rec := range recs {
		stale := rec.Status == statusRunning || rec.Status == statusWaiting
		if stale {
			rec.Status = statusIdle
		}
		queue, spilled := rec.Queue, rec.QueueSpilled
		rec.Queue, rec.QueueSpilled = nil, false
		s := a.addSession(rec, false, nil)
		unsent := false
		if !rec.closed() {
			if spilled {
				if queue, err = readSpilledQueue(a.stateDir, rec.ID); err != nil {
					fmt.Fprintln(os.Stderr, "agents: read spilled queue:", err)
				}
			}
			queue, unsent = restoredQueue(a.stateDir, rec.ID, queue)
			s.restoreQueue(queue)
		}
		if stale {
			a.notes.mark(s, changeStatus, true)
			if rec.Night == "" {
				cut[s] = cutOffText
				if unsent {
					cut[s] = ""
				}
			}
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
	reviewsAfter, err := a.loadReviews(ctx, h)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: load reviews:", err)
	}
	if _, err := h.Subscribe(ctx, reviewTopic+","+editorReady, reviewsAfter); err != nil {
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
	a.reapNight(ctx)
	a.requeueLandPrompts()
	close(a.loaded)
	a.resumeClones()
	go a.landingAtStart()
	if err := a.renderSettings(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render settings:", err)
	}
	go a.suspendIdleLoop(envDelay(suspendAfterEnv, suspendAfter))
	go a.closeDoneLoop(envDelay(closeAfterEnv, closeAfter))
	go a.deleteOldLoop(envDelay(deleteAfterEnv, deleteAfter))
	go a.prCheckLoop(envDelay(prCheckEnv, prCheckEvery))
	go a.ageOpenLoop(envDelay(openAfterEnv, openAfter))
	go a.continueCutOff(cut)
	go a.ensureSelf(context.WithoutCancel(ctx))
	go a.nightLoop()
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
			a.branches.reset(p.Workspace)
			a.kick(p.Workspace)
		}
	case editorFileTopic:
		var f editorFile
		if json.Unmarshal(ev.Payload, &f) == nil && a.setEditorFile(f) {
			a.kick(f.Workspace)
		}
	case reviewTopic:
		if ws, people, ok := a.reviews.apply(ev.Payload); ok {
			a.reviewsChanged(ctx, ev.Seq, map[string][]string{ws: people})
		}
	case editorReady:
		a.reviewsChanged(ctx, ev.Seq, a.reviews.closeAll())
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
			logError("forget sessions", a.sessionRevs.Forget(ctx, sessionsTracker(p.Workspace)))
			go a.dropWorkspace(p.Workspace, nil, true)
		}
	case contract.TopicWorkspaceUpdated:
		var p contract.WorkspaceChanged
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.kick(p.Workspace)
			go a.dropWorkspace(p.Workspace, a.worktreeGone, false)
			go a.offerRepos(p.Workspace)
			go a.cleanupOnSeen(p.Workspace)
		}
	}
}

func (a *app) addSession(rec record, loaded bool, ready *setup) *session {
	var s *session
	s = newSession(rec, newEventLog(logPath(a.stateDir, rec.ID)), loaded, func(kind string, save bool) {
		if kind == changeStatus {
			a.wakeWaiters()
			a.statusChanged(s)
			if child := s.snapshot(); child.RepoPath != "" && child.ParentSession != "" {
				go a.landingChanged()
			}
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
	s.began = a.notes.superseded
	s.requested = func(kind, request string) {
		a.notes.superseded(s.snapshot().ID)
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
			out = append(out, a.reminders.present(rec))
		}
	}
	slices.SortFunc(out, func(x, y record) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return out
}

func (a *app) infos(workspaceID string) []sessionInfo {
	recs := a.list(workspaceID)
	children := childStatuses(recs)
	kids := map[string][]record{}
	for _, rec := range recs {
		if rec.ParentSession != "" {
			kids[rec.ParentSession] = append(kids[rec.ParentSession], rec)
		}
	}
	out := make([]sessionInfo, 0, len(recs))
	for _, rec := range recs {
		info := sessionInfo{record: rec, NotifyEffective: a.effectiveNotify(rec), ChildStatus: children[rec.ID]}
		if s := a.session(rec.ID); s != nil {
			info.Pending = s.pending()
			info.Asks = s.asks()
			info.PendingSummary = s.permissionSummary()
			info.ModeLabel = s.modeLabel()
			info.Turns = s.turnsSoFar()
			if started := s.turnStart().UTC(); rec.Status == statusRunning && !started.IsZero() {
				info.TurnStartedAt = &started
			}
		}
		if at, ok := a.reminders.at(rec.Workspace, rec.ID); ok && rec.Reminding {
			info.Activity, info.TurnStartedAt = &toolActivity{Title: reminderActivity(at), At: rec.UpdatedAt}, nil
		}
		if info.ModeLabel == "" {
			info.ModeLabel = modeWord(rec.Mode, nil)
		}
		info.ReviewProgress = a.reviews.progress(rec.ID)
		info.NeedsYou = needsYou(info.record)
		info.Gate = a.gateState(info.record)
		info.Service = rec.restricted()
		if rec.Status == statusError {
			failure := failureOf(rec)
			a.uiMu.Lock()
			needsAuth, cannotResume := a.recoveryLocked(rec)
			a.uiMu.Unlock()
			failure.Actions = failureActions(rec.ID, needsAuth, cannotResume)
			info.Failure = &failure
		}
		if rec.closed() && rec.HandoffTo == "" {
			a.uiMu.Lock()
			_, cannotResume := a.recoveryLocked(rec)
			a.uiMu.Unlock()
			reopen := reopenAction(rec.ID, cannotResume, ownsTask(rec))
			info.Reopen = &reopen
		}
		if rec.connectWaits() {
			info.Pending = append(info.Pending, rec.connectIDs()...)
			info.PendingSummary = rec.connectSummary()
		}
		if rec.taskWaits() {
			info.Pending = append(info.Pending, rec.Task.requests()...)
			if info.PendingSummary == nil {
				info.PendingSummary = rec.taskPending(info.Asks)
			}
		}
		if rec.gateWaits() {
			info.Pending = append(info.Pending, rec.gateRequests()...)
			if info.PendingSummary == nil {
				info.PendingSummary = rec.gatePending()
			}
		}
		if rec.RepoPath != "" {
			info.Repo = filepath.Base(rec.RepoPath)
		}
		if rec.Task != nil {
			info.TaskSummary, _ = repoSummary(rec, kids[rec.ID])
		}
		info.withTask(kids[rec.ID])
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
