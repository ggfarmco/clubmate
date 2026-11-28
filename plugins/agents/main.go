package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
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
	host         *pluginsdk.Host
	stateDir     string
	sendHold     time.Duration
	takeHold     time.Duration
	closeGate    string
	roundAfter   time.Duration
	olderAfter   time.Duration
	loaded       chan struct{}
	quit         chan struct{}
	halting      atomic.Bool
	notes        *notifier
	groups       *groups
	codePanel    bool
	wikiPanel    bool
	editorReview atomic.Pointer[string]
	usage        usageTracker
	branches     branchCache
	baseCache    baseCache
	unlandedMu   sync.Mutex
	unlandedSeen map[string]unlandedVerdict
	texts        textIndex
	found        searchCache
	thumbs       thumbCache
	watchdog     watchdog
	overlapCache overlapCache
	roster       roster
	reviews      *reviewBoard
	self         *selfStore

	sessionRevs    delta.Observed
	runNeeds       runNeeds
	trustNeeds     trustNeeds
	graphs         *graphs.Engine
	runSigsMu      sync.Mutex
	runSigs        map[string]map[string]string
	reminders      reminders
	limitJobs      limitJobs
	taskLookups    taskLookups
	openWake       chan struct{}
	inbox          inboxMemo
	setup          setupCheck
	firstRun       firstRun
	companionState companionWatch

	spawnMu        sync.Mutex
	asideMu        sync.Mutex
	leaving        map[string]int
	worktreeMu     sync.Mutex
	connectMu      sync.Mutex
	cloners        map[string]bool
	landMu         sync.Mutex
	gateMu         sync.Mutex
	gatePrep       map[string]bool
	queues         landQueues
	resources      slotBoard
	gateTreeRun    func(ctx context.Context, workspaceID string, args map[string]any, out any) error
	gateStart      func(ctx context.Context, gr graphs.GateRun) (string, string, error)
	gateRuns       sync.Map
	docsRun        func(ctx context.Context, id, workspaceID string, args map[string]any) error
	gateLogMu      sync.Mutex
	historyMu      sync.Mutex
	historyCache   map[string][]gateVerdict
	outcomesMu     sync.Mutex
	journaled      map[string][sha256.Size]byte
	liveGates      atomic.Pointer[map[string]bool]
	heldFacts      heldFacts
	pauses         gatePauses
	lastGates      atomic.Pointer[[]gateConfig]
	lastGatesSaved atomic.Bool
	lastGatesMu    sync.Mutex
	gatesGen       atomic.Uint64
	rechecks       []time.Duration
	trustPoll      time.Duration
	trustPolls     sync.Map
	gateRepos      sync.Map
	hookMu         sync.Mutex
	hookPrimaries  sync.Map
	landWork       coalesced
	onceLocks      onceLocks
	archiving      archiveClaims
	declareMu      sync.Mutex
	nightMu        sync.Mutex
	stopOnce       sync.Once

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
	err := pluginsdk.Run(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed, Shutdown: a.shutdown})
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
	a.rechecks = recheckDelays(envDelay(recheckEnv, 0))
	a.trustPoll = envDelay(trustPollEnv, trustPollDefault)
	a.takeHold = envDelay(takeHoldEnv, 0)
	quizHold = envDelay(quizHoldEnv, quizHold)
	a.closeGate = os.Getenv(closeGateEnv)
	a.roundAfter = envDelay(roundResetEnv, roundReset)
	a.olderAfter = envDelay(olderAfterEnv, olderAfter)
	a.groups = newGroups(a.stateDir)
	a.groups.reap(orphanMarkers())
	a.self = newSelfStore(a.stateDir)
	a.graphs = a.newGraphs(h)
	installHooks(a.stateDir)
	a.loadSlots(ctx)
	logError("write the tool hooks", a.syncToolHooks(ctx))
	a.sweepToolHooks(time.Now())
	go a.hookLoop(envDelay(toolHookPollEnv, toolHookPoll))
	var sections []sectionRef
	if err := h.Call(ctx, "ui.sections", nil, &sections); err != nil {
		return err
	}
	a.usage.onShare, a.usage.tree = a.sharesChanged, a.spendTree
	if err := a.usage.load(ctx, h); err != nil {
		return err
	}
	a.codePanel = slices.ContainsFunc(sections, func(s sectionRef) bool { return s.ID == editorSection })
	a.loadEditorReview(ctx)
	a.wikiPanel = slices.ContainsFunc(sections, func(s sectionRef) bool { return s.ID == wikiSection })
	recs, err := loadRecords(ctx, h)
	if err != nil {
		return err
	}
	if err := a.firstRun.load(ctx, h, len(recs) > 0); err != nil {
		return err
	}
	cut := map[*session]string{}
	for _, rec := range recs {
		if !busy(rec.Status) {
			rec.Held = nil
		}
		stale := busy(rec.Status) && rec.Held == nil
		if stale {
			rec.Status = statusIdle
		}
		if rec.Held != nil {
			rec.Status = statusRunning
			a.watchdog.heldAtStart(rec.ID)
		}
		cleared := rec.clearCalls()
		queue, spilled := rec.Queue, rec.QueueSpilled
		rec.Queue, rec.QueueSpilled = nil, false
		s := a.addSession(rec, false, nil)
		s.unmarkCalls(cleared)
		unsent := false
		if !rec.closed() {
			if spilled {
				if queue, err = readSpilledQueue(a.stateDir, rec.ID); err != nil {
					fmt.Fprintln(os.Stderr, "agents: read spilled queue:", err)
				}
			}
			stored := slices.Clone(queue)
			queue, unsent = restoredQueue(a.stateDir, rec.ID, queue)
			s.restoreQueue(queue)
			if s.settleDelivered(stored, queue) {
				a.notes.mark(s, changeStatus, true)
			}
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
	a.sweepInputs()
	a.restoreSlots(ctx)
	go a.slotLoop()
	go a.texts.warm(a.stateDir, recs)
	go a.reconcileLimitWaits()
	go a.seedGateRuns()
	go a.dropClosedGateTrees()
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
	if _, err := h.Subscribe(ctx, pluginChangedTopic, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, editorFileTopic, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, currentReviewTopic+","+wikiPageTopic, 0); err != nil {
		return err
	}
	reviewsAfter, err := a.loadReviews(ctx, h)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: load reviews:", err)
	}
	if _, err := h.Subscribe(ctx, reviewTopic+","+editorReady, reviewsAfter); err != nil {
		return err
	}
	for _, topic := range []string{docsOpenedTopic, docsPresentedTopic, docsReadyTopic} {
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
	if _, err := h.Subscribe(ctx, remoteChangedTopic, ready.Seq); err != nil {
		return err
	}
	if _, err := h.Subscribe(ctx, contract.TopicRemoteWatching, ready.Seq); err != nil {
		return err
	}
	if err := a.loadPresence(ctx); err != nil {
		return err
	}
	a.reapNight(ctx)
	a.requeueLandPrompts()
	if err := a.restoreQueues(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: restore landing queues:", err)
	}
	close(a.loaded)
	if err := a.loadSetupFixes(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: load setup fixes:", err)
	}
	a.recheckSetup()
	a.resumeClones()
	go a.landingAtStart()
	if err := a.renderSettings(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render settings:", err)
	}
	go a.suspendIdleLoop(envDelay(suspendAfterEnv, suspendAfter))
	go a.closeDoneLoop(envDelay(closeAfterEnv, closeAfter))
	go a.deleteOldLoop(envDelay(deleteAfterEnv, deleteAfter))
	go a.prCheckLoop(envDelay(prCheckEnv, prCheckEvery))
	go a.landQueueLoop(envDelay(queueLoopEnv, queueLoopEvery))
	go a.ageOpenLoop(envDelay(openAfterEnv, openAfter))
	go a.roundResetLoop(a.roundAfter)
	a.settleAllGatedDocs()
	go a.continueCutOff(cut)
	go a.ensureSelf(context.WithoutCancel(ctx))
	go a.nightLoop()
	a.renderAll(ctx)
	go a.loadCompanion(context.WithoutCancel(ctx))
	go a.startGraphs(ctx, ready.Seq)
	return nil
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, sub string, ev pluginsdk.Event) {
	if a.graphs.Owns(sub) {
		a.graphs.BusEvent(ctx, sub, ev)
		return
	}
	switch ev.Topic {
	case "session.exited":
		a.signInExited(ctx, ev)
		a.setupFixExited(ev)
	case "ui.opened", "ui.idle", "ui.section":
		a.presenceEvent(ctx, ev)
	case pluginChangedTopic:
		a.pluginChanged(ev.Payload)
	case gitChangedTopic:
		var p gitChanged
		if json.Unmarshal(ev.Payload, &p) == nil {
			a.branches.reset(p.Workspace)
			a.baseCache.reset(p.Workspace)
			a.kick(p.Workspace)
			go a.checkQueues(p.Workspace)
		}
	case editorFileTopic:
		var f editorFile
		if json.Unmarshal(ev.Payload, &f) == nil && a.setEditorFile(f) {
			a.kick(f.Workspace)
		}
	case currentReviewTopic:
		var c currentReview
		if json.Unmarshal(ev.Payload, &c) == nil && a.setCurrentReview(c) {
			a.kick(c.WorkspaceID)
		}
	case wikiPageTopic:
		var p wikiPage
		if json.Unmarshal(ev.Payload, &p) == nil && a.setWikiPage(p) {
			a.kick(p.Workspace)
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
			a.docPresented(docModes[ev.Topic], p)
		}
	case docsReadyTopic:
		a.settleAllGatedDocs()
	case remoteChangedTopic:
		go a.loadCompanion(context.Background())
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

func (a *app) busClosed(ctx context.Context, _ *pluginsdk.Host, sub string, err error) {
	if a.graphs.Owns(sub) {
		a.graphs.BusClosed(ctx, sub, err)
		return
	}
	fmt.Fprintf(os.Stderr, "agents: bus subscription %s closed: %v\n", sub, err)
}

func (a *app) addSession(rec record, loaded bool, ready *setup) *session {
	var s *session
	s = newSession(rec, newEventLog(logPath(a.stateDir, rec.ID)), loaded, func(kind string, save bool) {
		if kind == changeStatus {
			a.holdSynced(s)
			a.cardsSynced(s)
			if s.trustFlipped() {
				a.trustSynced(rec.Workspace)
			}
			a.wakeWaiters()
			a.statusChanged(s)
			a.settleGatedDocs(s)
			if child := s.snapshot(); child.RepoPath != "" && child.ParentSession != "" {
				go a.landingChanged()
			}
		}
		a.notes.mark(s, kind, save)
		a.kick(rec.Workspace)
	})
	s.holdOf = heldCopies(rec)
	s.pauses = &a.pauses
	s.cards = rec.openCards()
	a.trustNeeds.seed(rec)
	s.trustOf = strings.Join(waitsForTrust(rec), ",")
	s.setup = ready
	s.wantTasks = func() { a.wantTasks(rec.Workspace, s) }
	s.turned = func(t api.Turn) {
		t.Finished = a.finished(t)
		a.notes.turn(t)
	}
	s.idled = func() { a.settleBackground(s) }
	s.holdCall = func(ctx context.Context, p *agentProc, request string, req acp.RequestPermissionRequest) (acp.PermissionOutcome, bool) {
		return a.holdCall(ctx, s, p, request, req)
	}
	s.withdrawCall = func(request string) bool { return a.withdrawCall(s, request) }
	s.cycled = func() {
		s.mu.Lock()
		s.dropping++
		s.mu.Unlock()
		go func() {
			a.dropEnded(s)
			s.mu.Lock()
			s.dropping--
			owed := s.dropping == 0 && s.followOwed
			if owed {
				s.followOwed = false
				s.followedUpLocked()
			}
			s.mu.Unlock()
			a.cycleStarted(s)
			if owed {
				a.settleBackground(s)
			}
		}()
	}
	s.ran = func(c endedCall) { a.judgeCall(s, c) }
	s.slotted = func(c slotCall) { a.slotCallEnded(rec.ID, c) }
	s.queueEdit = func(c editedCall) { a.warnEdit(s, c) }
	var tree treeKeyCache
	s.moved = func() { a.overlapCache.move(tree.of(s.snapshot().Worktree)) }
	s.toolGated = func() bool {
		last := a.lastGates.Load()
		return last == nil || len(toolGatedRepos(*last)) > 0
	}
	s.resolved = func(kind, request string) { a.notes.resolved(s, kind, request) }
	s.began = a.notes.superseded
	s.requested = func(kind, request string) {
		if kind == entryPermission {
			a.firstPermission(rec.ID, request)
		}
		a.notes.superseded(s.snapshot().ID)
		if kind != entryElicitation || !a.asksParent(s, s.snapshot()) {
			a.notes.requested(s, kind, request)
		}
	}
	a.mu.Lock()
	a.sessions[rec.ID] = s
	a.mu.Unlock()
	recordsRev.Add(1)
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

func (a *app) presentRecord(rec *record) {
	rec.AsksParent = false
	if rec.Status == statusWaiting && rec.ParentSession != "" {
		if s := a.session(rec.ID); s != nil {
			rec.AsksParent = a.asksParent(s, *rec)
		}
	}
	if rec.Status == statusDone {
		*rec = a.reminders.present(*rec)
	}
}

func (a *app) list(workspaceID string) []record {
	out := a.roster.records(a, workspaceID, nil)
	for i := range out {
		a.presentRecord(&out[i])
	}
	return out
}

func (a *app) listByWorkspace() map[string][]record {
	workspaces := map[string]bool{}
	a.roster.each(a, func(rec *record) { workspaces[rec.Workspace] = true })
	out := map[string][]record{}
	for id := range workspaces {
		if id != "" {
			out[id] = a.list(id)
		}
	}
	return out
}

func (a *app) infos(workspaceID string) []sessionInfo {
	return a.infosOf(workspaceID, a.list(workspaceID))
}

func (a *app) infosOf(workspaceID string, recs []record) []sessionInfo {
	children := childStatuses(recs, a.gateRunOpen)
	kids := kidsOf(recs)
	nest := reviewNests(recs)
	tree := a.spendTree()
	out := make([]sessionInfo, 0, len(recs))
	for _, rec := range recs {
		rec.DocIndex, rec.NeighboursTold, rec.QueueTold, rec.LandOverlap = nil, nil, nil, nil
		info := baseInfo(rec, nest)
		info.NotifyEffective, info.ChildStatus = a.effectiveNotify(rec), children[rec.ID]
		if s := a.session(rec.ID); s != nil {
			info.Pending = s.pending()
			info.Asks = s.asks()
			info.PendingSummary = s.permissionSummary()
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
		if share := a.usage.sessionShare(tree.of(rec.ID)); len(share) > 0 {
			info.LimitShare, info.ShareLabel, info.ShareWords, info.ShareDetail = share, shareLabel(share), shareWords(share), shareDetail(share)
		}
		info.Gate = a.gateState(info.record)
		info.Landing = a.landing(info.record)
		info.LandQueue = a.queuePlaceOf(rec.ID)
		info.SlotWait, info.SlotHold = a.slotView(rec.ID)
		info.Ungated = a.ungated(info.record)
		info.TurnHeld = a.turnHeld(info.record)
		info.CallHeld = info.record.callHeld()
		info.Calls = a.callViews(info.record)
		info.HeldRound = a.heldRound(info.record)
		info.Aside = asideViewOf(rec)
		info.CanAside = a.canAside(rec)
		info.Suggestion = a.suggestionFor(rec)
		if rec.Status == statusError {
			failure := failureOf(rec)
			a.uiMu.Lock()
			needsAuth, cannotResume := a.recoveryLocked(rec)
			a.uiMu.Unlock()
			failure.Actions = failureActions(rec.ID, needsAuth, cannotResume, rec.LimitWait.pausing())
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
			info.CardRequests = rec.gateRequests()
			info.Pending = append(info.Pending, info.CardRequests...)
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
	out = append(out, a.needRows(workspaceID, recs)...)
	setGroups(out)
	a.inbox.apply(workspaceID, out)
	return out
}

func (a *app) needRows(workspaceID string, recs []record) []sessionInfo {
	out := a.runRows(workspaceID, recs, func(info *sessionInfo, lander record) {
		if l := a.landing(lander); l != nil {
			info.GateRound = &gateRound{Round: l.Round, Label: fmt.Sprintf(reviewRoundLabel, l.Round), Verdict: gateStateReview}
			info.Title = info.GateRound.Label
		}
	})
	for _, n := range trustNeedsOf(recs) {
		n.Name, n.Point = a.trustGate(n.Gate)
		out = append(out, trustInfo(workspaceID, n))
	}
	return out
}

func baseInfo(rec record, nest map[string]reviewerNest) sessionInfo {
	info := sessionInfo{record: rec, NeedsYou: needsYou(rec), Service: rec.restricted()}
	if n, ok := nest[rec.ID]; ok {
		info.applyNest(n)
	}
	return info
}

func (a *app) runRows(workspaceID string, recs []record, under func(*sessionInfo, record)) []sessionInfo {
	askers := a.gateAskers(recs)
	var out []sessionInfo
	for _, n := range a.runNeeds.list(workspaceID) {
		info := runInfo(workspaceID, n)
		if lander, ok := askers[n.Run]; ok {
			info.RowUnder, info.NestUnder = lander.ID, lander.ID
			if under != nil {
				under(&info, lander)
			}
		}
		out = append(out, info)
	}
	return out
}

func (i *sessionInfo) applyNest(n reviewerNest) {
	round := n.Round
	i.GateRound, i.Hidden = &round, !n.Shown
	if n.Tracked {
		i.NestUnder = n.Lander
	}
	if n.Shown && n.Open {
		i.RowUnder = n.Lander
	}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}
