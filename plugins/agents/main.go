package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
)

const (
	changedTopic    = "agents.changed"
	remotePlugin    = "remote"
	sessionKey      = "session/"
	modeKey         = "mode/"
	configKey       = "config/"
	advisorKey      = "advisor/"
	advisorID       = "advisor"
	finishKey       = "finish/default"
	finishID        = "finish"
	finishAsk       = "ask"
	finishMerge     = "merge"
	checktimeCmd    = "editor-nvim.checktime"
	attachTimeout   = 2 * time.Minute
	callTimeout     = 10 * time.Second
	refsTimeout     = 30 * time.Second
	checkpointLimit = 2 * time.Minute
	embedLimit      = 1 << 20
	stopRootWait    = 4 * time.Second
	worktreeLimit   = 10 * time.Minute
	worktreeNewCmd  = "git.worktree-new"
	worktreeOpCmd   = "git.worktree-op-status"
	worktreesDir    = ".worktrees"
	maxChildren     = 8
	unknownCaller   = "unknown"
	fromFormat      = "[From Clubmate agent session \"%s\" (%s), not from the user]\n\n"
	fromUnknown     = "[From another agent session, not from the user]\n\n"
)

var (
	errHome          = errors.New("Agents aren't available in the home workspace")
	errNoFolder      = errors.New("the workspace has no folders")
	errNoSession     = errors.New("unknown agent session")
	errClosed        = errors.New("the session is closed")
	errNotLive       = errors.New("the session's agent is not running")
	errNoResume      = errors.New("the agent cannot resume this session")
	errUnknownMode   = errors.New("unknown mode")
	errUnknownValue  = errors.New("unknown config value")
	errUnknownCmd    = errors.New("unknown command")
	errNoStateDir    = errors.New("no plugin state dir")
	errShutdown      = errors.New("the agents plugin is shutting down")
	errRelativeRoot  = errors.New("stop-root: root must be absolute")
	errNotDescendant = errors.New("the session was not started by this agent")
	errNotGit        = errors.New("the worktree is not in a git folder of the workspace")
	errBadBranch     = errors.New("not a valid branch name")
	errChildLimit    = fmt.Errorf("a session may run at most %d child sessions at once", maxChildren)
)

type sessionArgs struct {
	Session string `json:"session"`
}

type newArgs struct {
	Agent  string `json:"agent"`
	Branch string `json:"branch"`
	Prompt string `json:"prompt"`
	Title  string `json:"title"`
}

type attachment struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
}

type promptArgs struct {
	Session     string       `json:"session"`
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments"`
	From        string       `json:"-"`
}

type answerArgs struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

type permissionArgs struct {
	Session string `json:"session"`
	Request string `json:"request"`
	Option  string `json:"option"`
}

type modeArgs struct {
	Session string `json:"session"`
	Mode    string `json:"mode"`
}

type configArgs struct {
	Session string `json:"session"`
	ID      string `json:"id"`
	Value   string `json:"value"`
}

type finishArgs struct {
	Value string `json:"value"`
}

type finishResult struct {
	Value string `json:"value"`
}

type rootArgs struct {
	Root string `json:"root"`
}

type transcriptArgs struct {
	Session  string `json:"session"`
	FromTurn int    `json:"fromTurn"`
}

type newResult struct {
	Session  string `json:"session"`
	Worktree string `json:"worktree"`
	parent   string
}

type worktreeNewArgs struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	From   string `json:"from"`
	Switch bool   `json:"switch"`
}

type worktreeOp struct {
	ID    string `json:"id"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
	Path  string `json:"path"`
}

type projectPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type app struct {
	host     *pluginsdk.Host
	stateDir string
	loaded   chan struct{}
	notes    *notifier
	groups   *groups

	uiMu         sync.Mutex
	ui           map[string]*projectUI
	auth         map[string]authCommand
	signIns      map[string]string
	nodeProblem  string
	installing   bool
	goneProjects map[string]bool
	resumable    map[string]bool
	steers       map[string]bool
	noResume     map[string]bool
	advisors     map[string]string

	spawnMu    sync.Mutex
	worktreeMu sync.Mutex

	wakeMu sync.Mutex
	woken  chan struct{}

	mu       sync.Mutex
	sessions map[string]*session
	procs    map[string]*procSlot
	live     map[*agentProc]bool
	dropped  map[string]bool
	closing  bool
	stopOnce sync.Once
}

func main() {
	a := &app{
		loaded: make(chan struct{}), sessions: map[string]*session{}, procs: map[string]*procSlot{}, live: map[*agentProc]bool{}, dropped: map[string]bool{},
		ui: map[string]*projectUI{}, auth: map[string]authCommand{}, signIns: map[string]string{},
		goneProjects: map[string]bool{}, resumable: map[string]bool{}, steers: map[string]bool{}, noResume: map[string]bool{}, advisors: map[string]string{},
	}
	signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM)
	err := pluginsdk.Run(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent, Shutdown: a.shutdown})
	a.stopAll()
	if err != nil {
		os.Exit(1)
	}
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
	keys, err := h.StorageList(ctx, sessionKey)
	if err != nil {
		return err
	}
	for _, k := range keys {
		var rec record
		if found, err := h.StorageGet(ctx, k, &rec); err != nil || !found || !sessionIDPattern.MatchString(rec.ID) {
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
	a.renderAll(ctx)
	return nil
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

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.loaded:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch cmd.ID {
	case "agents.new":
		var args newArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		res, err := a.newSession(ctx, h, cmd.ProjectID, origin(cmd), args)
		if err != nil {
			return nil, err
		}
		if res.parent == "" {
			if err := a.selectSession(ctx, cmd.ProjectID, res.Session); err != nil {
				return nil, err
			}
		}
		a.kick(cmd.ProjectID)
		return res, nil
	case "agents.prompt":
		var args promptArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		args.From = origin(cmd)
		return nil, a.prompt(cmd.ProjectID, args)
	case "agents.answer":
		var args answerArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.prompt(cmd.ProjectID, promptArgs{Session: args.Session, Text: args.Text})
	case "agents.permission":
		var args permissionArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		s, err := a.lookup(cmd.ProjectID, args.Session)
		if err != nil {
			return nil, err
		}
		user := !cmd.Agent && cmd.Via == remotePlugin
		if !user && (cmd.Caller == "" || !a.descendantOf(args.Session, cmd.Caller)) {
			return nil, errNotDescendant
		}
		return nil, s.answer(args.Request, args.Option)
	case "agents.mode":
		var args modeArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.setMode(ctx, cmd.ProjectID, args)
	case "agents.config":
		var args configArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.setConfig(ctx, cmd.ProjectID, args)
	case "agents.sessions":
		return a.infos(cmd.ProjectID), nil
	case "agents.finishDefault":
		var args finishArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.finishCommand(ctx, args)
	case "agents.stop-root":
		var args rootArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(args.Root) {
			return nil, errRelativeRoot
		}
		return nil, a.stopRoot(ctx, filepath.Clean(args.Root))
	case "agents.wait":
		var args waitArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.wait(ctx, cmd.ProjectID, cmd.Caller, args)
	case "agents.transcript":
		var args transcriptArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		s, err := a.lookup(cmd.ProjectID, args.Session)
		if err != nil {
			return nil, err
		}
		return s.view(args.FromTurn), nil
	}
	var args sessionArgs
	if err := decode(cmd.Args, &args); err != nil {
		return nil, err
	}
	s, err := a.lookup(cmd.ProjectID, args.Session)
	if err != nil {
		return nil, err
	}
	switch cmd.ID {
	case "agents.cancel":
		if cmd.Agent && !a.descendantOf(args.Session, cmd.Caller) {
			return nil, errNotDescendant
		}
		a.cancel(s)
		return nil, nil
	case "agents.close":
		a.closeTree(ctx, s)
		return nil, nil
	case "agents.delete":
		return nil, a.deleteTree(ctx, s)
	case "agents.resume":
		a.resume(s)
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %s", errUnknownCmd, cmd.ID)
}

func (a *app) finishCommand(ctx context.Context, args finishArgs) (finishResult, error) {
	if args.Value == "" {
		return finishResult{Value: a.finishDefault(ctx)}, nil
	}
	if !validFinish(args.Value) {
		return finishResult{}, fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, args.Value)
	}
	return finishResult{Value: args.Value}, a.host.StorageSet(ctx, finishKey, args.Value)
}

func (a *app) finishDefault(ctx context.Context) string {
	var value string
	if found, err := a.host.StorageGet(ctx, finishKey, &value); err != nil || !found || !validFinish(value) {
		return finishAsk
	}
	return value
}

func validFinish(value string) bool {
	return slices.ContainsFunc(finishChoices, func(c advisorChoice) bool { return c.value == value })
}

func (a *app) setFinish(s *session, value string) error {
	if !validFinish(value) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, finishID, value)
	}
	s.mu.Lock()
	s.rec.Finish = value
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return nil
}

func (a *app) newSession(ctx context.Context, h *pluginsdk.Host, projectID, from string, args newArgs) (newResult, error) {
	agent := cmp.Or(args.Agent, agentClaude)
	if agent != agentClaude {
		return newResult{}, fmt.Errorf("%w: %s", errUnknownAgent, agent)
	}
	p, err := h.GetProject(ctx, projectID)
	if err != nil {
		return newResult{}, err
	}
	if p.Home {
		return newResult{}, errHome
	}
	root, _, err := workRoot(p)
	if err != nil {
		return newResult{}, err
	}
	rec := record{Workspace: projectID, Agent: agent, Title: titleOf(args.Title), TitleSet: args.Title != ""}
	if parent, err := a.lookup(projectID, from); err == nil {
		prec := parent.snapshot()
		rec.ParentSession, rec.Advisor, root = prec.ID, a.sessionAdvisor(ctx, prec), prec.Worktree
	}
	var plan worktreeNewArgs
	var ready *setup
	if args.Branch != "" {
		if plan, root, err = planWorktree(ctx, p, root, args.Branch); err != nil {
			return newResult{}, err
		}
		ready = newSetup(plan)
	}
	rec.Worktree = root
	s, err := a.spawn(rec, promptArgs{Text: args.Prompt, From: from}, ready)
	if ready != nil && s != nil {
		go a.createWorktree(s, ready)
	}
	if err != nil {
		return newResult{}, err
	}
	return newResult{Session: s.rec.ID, Worktree: root, parent: rec.ParentSession}, nil
}

func (a *app) spawn(rec record, first promptArgs, ready *setup) (*session, error) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if rec.ParentSession != "" {
		if parent := a.session(rec.ParentSession); parent == nil || parent.snapshot().Closed {
			return nil, errClosed
		}
		if a.runningChildren(rec.ParentSession) >= maxChildren {
			return nil, errChildLimit
		}
	}
	s := a.createSession(rec, ready)
	if first.Text == "" {
		return s, nil
	}
	first.Session = s.rec.ID
	return s, a.submit(s, &first)
}

func (a *app) createSession(rec record, ready *setup) *session {
	now := time.Now().UTC()
	rec.ID, rec.Finish, rec.Status, rec.CreatedAt, rec.UpdatedAt = newSessionID(), a.finishDefault(context.Background()), statusIdle, now, now
	s := a.addSession(rec, true, ready)
	a.notes.mark(s, changeCreated, true)
	a.kick(rec.Workspace)
	go a.attachInBackground(s)
	return s
}

func planWorktree(ctx context.Context, p pluginsdk.Project, base, branch string) (worktreeNewArgs, string, error) {
	if !gitrepo.ValidBranchName(branch) {
		return worktreeNewArgs{}, "", fmt.Errorf("%w: %s", errBadBranch, branch)
	}
	top, ok := gitrepo.RepoRoot(base)
	if !ok {
		return worktreeNewArgs{}, "", errNotGit
	}
	for _, repo := range p.Repos {
		f, err := gitrepo.OpenFolder(ctx, repo)
		if err != nil {
			continue
		}
		if _, ok := f.Member(top); ok {
			dir := filepath.Join(f.Trees[0].Path, worktreesDir, branch, f.Rel)
			return worktreeNewArgs{Repo: repo, Branch: branch, From: top}, dir, nil
		}
	}
	return worktreeNewArgs{}, "", errNotGit
}

func (a *app) createWorktree(s *session, ready *setup) {
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), worktreeLimit)
	defer cancel()
	err := a.newWorktree(ctx, s.snapshot().Workspace, ready.args)
	if err == nil {
		a.setFork(ctx, s)
	}
	ready.finish(err)
}

func (a *app) newWorktree(ctx context.Context, projectID string, args worktreeNewArgs) error {
	var op worktreeOp
	if err := a.host.RunCommand(ctx, worktreeNewCmd, projectID, args, &op); err != nil {
		return fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	if err := a.awaitOp(ctx, projectID, op.ID); err != nil {
		return fmt.Errorf("create worktree %s: %w", args.Branch, err)
	}
	return nil
}

func (a *app) children(id string) []*session {
	a.mu.Lock()
	all := slices.Collect(maps.Values(a.sessions))
	a.mu.Unlock()
	var out []*session
	for _, s := range all {
		if s.snapshot().ParentSession == id {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(x, y *session) int { return x.snapshot().CreatedAt.Compare(y.snapshot().CreatedAt) })
	return out
}

func (a *app) runningChildren(id string) int {
	n := 0
	for _, c := range a.children(id) {
		if st := c.snapshot().Status; st == statusRunning || st == statusWaiting {
			n++
		}
	}
	return n
}

func (a *app) descendantOf(id, ancestor string) bool {
	if id == ancestor {
		return false
	}
	seen := map[string]bool{}
	for !seen[id] {
		seen[id] = true
		s := a.session(id)
		if s == nil {
			return false
		}
		id = s.snapshot().ParentSession
		if id == "" {
			return false
		}
		if id == ancestor {
			return true
		}
	}
	return false
}

func workRoot(p pluginsdk.Project) (string, []string, error) {
	if len(p.Roots) == 0 {
		return "", nil, errNoFolder
	}
	i := slices.IndexFunc(p.Roots, func(r string) bool {
		_, ok := gitrepo.RepoRoot(r)
		return ok
	})
	i = max(i, 0)
	return p.Roots[i], slices.Delete(slices.Clone(p.Roots), i, i+1), nil
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
		info := sessionInfo{record: rec}
		if s := a.session(rec.ID); s != nil {
			info.Pending = s.pending()
			info.Asks = s.asks()
		}
		out = append(out, info)
	}
	return out
}

func (a *app) attachInBackground(s *session) {
	if _, err := a.attach(s); err != nil {
		a.fail(s, err.Error())
	}
}

func (a *app) fail(s *session, text string) {
	s.mu.Lock()
	if s.rec.Closed {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.recordLocked(actorClubmate, kindError, turnEndPayload{Error: text})
	s.setStatusLocked(statusError, text)
	ended := s.turnLocked()
	s.mu.Unlock()
	s.turned(ended)
	s.changed(changeStatus, true)
}

func (a *app) attach(s *session) (*agentProc, error) {
	if err := a.worktreeReady(s).wait(); err != nil {
		return nil, err
	}
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	s.loadLocked()
	p, rec, prompted := s.proc, s.rec, s.prompted
	s.mu.Unlock()
	if p != nil && p.alive() {
		return p, nil
	}
	if rec.Closed {
		return nil, errClosed
	}
	p, err := a.ensureProc(context.Background(), rec.Agent, rec.Workspace, rec.Worktree)
	a.setNodeProblem(err)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()
	var extra []string
	if p.caps.SessionCapabilities.AdditionalDirectories != nil {
		if project, err := a.host.GetProject(ctx, rec.Workspace); err == nil {
			_, extra, _ = workRoot(project)
		}
	}
	if rec.AgentSessionID != "" && prompted {
		return p, a.resumeOn(ctx, s, p, rec, extra)
	}
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionNew, acp.NewSessionRequest{Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcp, Meta: sessionMeta(rec.ID, a.sessionAdvisor(ctx, rec))}, &raw); err != nil {
		a.release(p)
		if authErr := a.authRequired(rec.ID, p, err); authErr != nil {
			return nil, authErr
		}
		return nil, fmt.Errorf("session/new: %w", err)
	}
	var res acp.NewSessionResponse
	if err := json.Unmarshal(raw, &res); err != nil || res.SessionID == "" {
		a.release(p)
		return nil, fmt.Errorf("session/new: bad response %s", raw)
	}
	s.mu.Lock()
	s.rec.AgentSessionID = res.SessionID
	if s.rec.Status == statusError {
		s.setStatusLocked(statusIdle, "")
	}
	s.mu.Unlock()
	a.notes.persist(ctx, s)
	p.register(res.SessionID, s)
	s.mu.Lock()
	s.recordLocked(s.actor(), kindNew, raw)
	s.mu.Unlock()
	s.changed(changeStatus, true)
	mode, value := a.startSettings(ctx, rec)
	a.applyMode(ctx, s, res.Modes, mode)
	a.applyConfigs(ctx, s, res.ConfigOptions, value)
	return p, nil
}

func (a *app) worktreeReady(s *session) *setup {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setup.failed() {
		s.setup = newSetup(s.setup.args)
		go a.createWorktree(s, s.setup)
	}
	return s.setup
}

func (a *app) resumeOn(ctx context.Context, s *session, p *agentProc, rec record, extra []string) error {
	if p.caps.SessionCapabilities.Resume == nil {
		a.release(p)
		a.setNoResume(rec.ID, true)
		return errNoResume
	}
	var raw json.RawMessage
	if err := p.conn.Call(ctx, acp.MethodSessionResume, acp.ResumeSessionRequest{SessionID: rec.AgentSessionID, Cwd: rec.Worktree, AdditionalDirectories: extra, McpServers: p.mcp, Meta: sessionMeta(rec.ID, a.sessionAdvisor(ctx, rec))}, &raw); err != nil {
		a.release(p)
		if authErr := a.authRequired(rec.ID, p, err); authErr != nil {
			return authErr
		}
		a.setNoResume(rec.ID, true)
		return fmt.Errorf("session/resume: %w", err)
	}
	a.setNoResume(rec.ID, false)
	p.register(rec.AgentSessionID, s)
	s.mu.Lock()
	s.recordLocked(s.actor(), kindResume, raw)
	if s.rec.Status == statusError {
		s.setStatusLocked(statusIdle, "")
	}
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return nil
}

func (a *app) startSettings(ctx context.Context, rec record) (string, func(id string) string) {
	if rec.ParentSession == "" {
		return a.stored(ctx, modeStorageKey(rec.Worktree)), func(id string) string { return a.stored(ctx, configStorageKey(id, rec.Worktree)) }
	}
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return "", func(string) string { return "" }
	}
	mode, values := parent.settings()
	return mode, func(id string) string { return values[id] }
}

func (a *app) stored(ctx context.Context, key string) string {
	var value string
	if found, err := a.host.StorageGet(ctx, key, &value); err != nil || !found {
		return ""
	}
	return value
}

func (a *app) applyMode(ctx context.Context, s *session, modes *acp.SessionModeState, mode string) {
	if modes == nil || mode == "" || mode == modes.CurrentModeID {
		return
	}
	if !slices.ContainsFunc(modes.AvailableModes, func(m acp.SessionMode) bool { return m.ID == mode }) {
		return
	}
	if err := a.sendMode(ctx, s, mode); err != nil {
		fmt.Fprintln(os.Stderr, "agents: restore mode:", err)
	}
}

func (a *app) applyConfigs(ctx context.Context, s *session, configs []acp.SessionConfigOption, valueOf func(id string) string) {
	for i := 0; i < len(configs); i++ {
		o := configs[i]
		if !pickable(o) {
			continue
		}
		value := valueOf(o.ID)
		if value == "" || value == o.Current() {
			continue
		}
		if _, ok := o.Choice(value); !ok {
			continue
		}
		next, err := a.sendConfig(ctx, s, o.ID, value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: restore config:", err)
			continue
		}
		if next != nil {
			configs = next
		}
	}
}

func (a *app) prompt(projectID string, args promptArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	if args.From == "" || s.snapshot().ParentSession != args.From {
		return a.submit(s, &args)
	}
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if st := s.snapshot().Status; st != statusRunning && st != statusWaiting && a.runningChildren(args.From) >= maxChildren {
		return errChildLimit
	}
	return a.submit(s, &args)
}

func origin(cmd pluginsdk.CommandRun) string {
	if cmd.Caller == "" && cmd.Agent {
		return unknownCaller
	}
	return cmd.Caller
}

func (a *app) submit(s *session, args *promptArgs) error {
	s.mu.Lock()
	s.loadLocked()
	if args != nil && s.running && !s.cancelRequested && !s.rec.Closed && s.proc != nil && s.proc.steering {
		p, rec := s.proc, s.rec
		s.mu.Unlock()
		if a.steer(s, p, rec, *args) {
			return nil
		}
		s.mu.Lock()
	}
	switch {
	case s.rec.Closed:
		s.mu.Unlock()
		return errClosed
	case s.running && args == nil:
		s.mu.Unlock()
		return nil
	case s.running:
		s.enqueueLocked(*args)
		s.mu.Unlock()
		s.changed(changeUpdate, false)
		a.wakeWaiters()
		return nil
	}
	if args != nil {
		s.enqueueLocked(*args)
	}
	next, ok := s.dequeueLocked()
	if !ok {
		s.mu.Unlock()
		return nil
	}
	turn := s.beginTurnLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	go a.runTurn(s, turn, next)
	return nil
}

func (a *app) steer(s *session, p *agentProc, rec record, args promptArgs) bool {
	blocks := promptBlocks(args, rec.Worktree, p.caps.PromptCapabilities.EmbeddedContext)
	req := acp.NewSteerRequest(rec.AgentSessionID, slices.Clone(blocks))
	if args.From != "" {
		req.Prompt[0].Text = a.fromPrefix(args.From) + req.Prompt[0].Text
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var res acp.SteerResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSteering, req, &res); err != nil {
		fmt.Fprintln(os.Stderr, "agents: steer:", err)
		return false
	}
	if res.Outcome != acp.SteerInjected {
		return false
	}
	s.mu.Lock()
	s.recordLocked(promptActor(args.From), kindPrompt, promptPayload{PromptRequest: acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: blocks}, From: args.From, Steered: true})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return true
}

func (a *app) runTurn(s *session, turn int, args promptArgs) {
	p, err := a.attach(s)
	if err != nil {
		s.mu.Lock()
		s.requeueLocked(args)
		s.mu.Unlock()
		a.fail(s, err.Error())
		return
	}
	if a.abandon(s, args) {
		return
	}
	rec := s.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	sha, err := checkpoint(ctx, rec.Worktree, rec.ID, turn)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: checkpoint:", err)
	}
	s.mu.Lock()
	deleted := s.deleted
	if !deleted {
		s.recordLocked(actorClubmate, kindCheckpoint, checkpointPayload{Turn: turn, Sha: sha})
	}
	s.mu.Unlock()
	if deleted {
		ctx, cancel := context.WithTimeout(context.Background(), refsTimeout)
		defer cancel()
		if err := deleteCheckpoints(ctx, rec.Worktree, rec.ID); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete checkpoints:", err)
		}
		return
	}
	if a.abandon(s, args) {
		return
	}
	req := acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: promptBlocks(args, rec.Worktree, p.caps.PromptCapabilities.EmbeddedContext)}
	s.mu.Lock()
	s.recordLocked(promptActor(args.From), kindPrompt, promptPayload{PromptRequest: req, From: args.From})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
	req.Prompt = append(slices.Clip(req.Prompt), a.policies(ctx, rec)...)
	cancel()
	if args.From != "" {
		req.Prompt[0].Text = a.fromPrefix(args.From) + req.Prompt[0].Text
	}
	var res acp.PromptResponse
	err = p.conn.Call(context.Background(), acp.MethodSessionPrompt, req, &res)
	if errors.Is(err, acp.ErrClosed) {
		go p.stop()
		return
	}
	s.mu.Lock()
	s.log.flush()
	s.running = false
	edited := s.edited
	var next promptArgs
	nextTurn, finished := 0, false
	switch {
	case s.rec.Closed:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
	case err != nil:
		s.recordLocked(actorClubmate, kindTurnEnd, turnEndPayload{Error: err.Error()})
		s.setStatusLocked(statusError, err.Error())
	case !s.cancelRequested && res.StopReason != acp.StopCancelled && len(s.queue) > 0:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		next, _ = s.dequeueLocked()
		nextTurn = s.beginTurnLocked()
	default:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		if s.rec.Status == statusWaiting {
			s.prior = statusDone
		} else {
			s.setStatusLocked(statusDone, "")
			finished = true
		}
	}
	closed, ended := s.rec.Closed, s.turnLocked()
	s.mu.Unlock()
	if !closed {
		s.turned(ended)
	}
	s.changed(changeStatus, true)
	if edited {
		a.checktime(rec)
	}
	if nextTurn > 0 {
		a.runTurn(s, nextTurn, next)
	}
	if finished {
		a.cleanupChild(s)
		a.closeVanishedChildren(s)
	}
}

func (a *app) checktime(rec record) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := a.host.RunCommand(ctx, checktimeCmd, rec.Workspace, map[string]string{"root": rec.Worktree}, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: checktime:", err)
	}
}

func (a *app) abandon(s *session, args promptArgs) bool {
	s.mu.Lock()
	switch {
	case s.deleted || s.rec.Closed:
		s.running = false
		s.mu.Unlock()
		return true
	case s.cancelRequested:
		s.running = false
		s.requeueLocked(args)
		s.recordLocked(actorUser, kindTurnEnd, turnEndPayload{StopReason: acp.StopCancelled})
		if s.rec.Status == statusWaiting {
			s.prior = statusDone
		} else {
			s.setStatusLocked(statusDone, "")
		}
		ended := s.turnLocked()
		s.mu.Unlock()
		s.turned(ended)
		s.changed(changeStatus, true)
		return true
	}
	s.mu.Unlock()
	return false
}

func promptActor(from string) string {
	if from == "" {
		return actorUser
	}
	return "agent:" + from
}

func (a *app) fromPrefix(id string) string {
	if id == unknownCaller {
		return fromUnknown
	}
	var title string
	if s := a.session(id); s != nil {
		title = s.snapshot().Title
	}
	return fmt.Sprintf(fromFormat, title, id)
}

func promptBlocks(args promptArgs, root string, embedded bool) []acp.ContentBlock {
	blocks := []acp.ContentBlock{{Type: "text", Text: args.Text}}
	for _, att := range args.Attachments {
		path := att.Path
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		switch att.Kind {
		case "file":
			if embedded {
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() <= embedLimit {
					if data, err := os.ReadFile(path); err == nil {
						blocks = append(blocks, acp.ContentBlock{Type: "resource", Resource: &acp.TextResourceContents{URI: uri, Text: strings.ToValidUTF8(string(data), "")}})
						continue
					}
				}
			}
			blocks = append(blocks, acp.ContentBlock{Type: "resource_link", URI: uri, Name: filepath.Base(path)})
		case "selection":
			lines := strconv.Itoa(att.StartLine) + "-" + strconv.Itoa(att.EndLine)
			if embedded {
				blocks = append(blocks, acp.ContentBlock{Type: "resource", Resource: &acp.TextResourceContents{URI: uri + "#L" + lines, Text: att.Text}})
				continue
			}
			blocks = append(blocks, acp.ContentBlock{Type: "text", Text: path + ":" + lines + "\n```\n" + att.Text + "\n```"})
		}
	}
	return blocks
}

func (a *app) cancel(s *session) {
	s.mu.Lock()
	p, running, agentID := s.proc, s.running, s.rec.AgentSessionID
	if running {
		s.cancelRequested = true
		s.recordLocked(actorUser, kindCancel, acp.SessionRequest{SessionID: agentID})
	}
	s.mu.Unlock()
	if p != nil && running {
		if err := p.conn.Notify(acp.MethodSessionCancel, acp.SessionRequest{SessionID: agentID}); err != nil {
			fmt.Fprintln(os.Stderr, "agents: cancel:", err)
		}
	}
	s.mu.Lock()
	s.cancelPermissionsLocked()
	s.mu.Unlock()
	s.changed(changeUpdate, false)
}

func (a *app) setMode(ctx context.Context, projectID string, args modeArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	s.mu.Lock()
	known := slices.ContainsFunc(s.modes, func(m acp.SessionMode) bool { return m.ID == args.Mode })
	worktree := s.rec.Worktree
	s.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: %s", errUnknownMode, args.Mode)
	}
	if err := a.sendMode(ctx, s, args.Mode); err != nil {
		return err
	}
	return a.host.StorageSet(ctx, modeStorageKey(worktree), args.Mode)
}

func (a *app) sendMode(ctx context.Context, s *session, mode string) error {
	s.mu.Lock()
	p, agentID := s.proc, s.rec.AgentSessionID
	s.mu.Unlock()
	if p == nil {
		return errNotLive
	}
	req := acp.SetSessionModeRequest{SessionID: agentID, ModeID: mode}
	if err := p.conn.Call(ctx, acp.MethodSessionSetMode, req, nil); err != nil {
		return fmt.Errorf("session/set_mode: %w", err)
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindSetMode, req)
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return nil
}

func (a *app) setConfig(ctx context.Context, projectID string, args configArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	switch args.ID {
	case advisorID:
		return a.setAdvisor(ctx, s, args.Value)
	case finishID:
		return a.setFinish(s, args.Value)
	}
	s.mu.Lock()
	i := slices.IndexFunc(s.configs, func(o acp.SessionConfigOption) bool { return o.ID == args.ID && pickable(o) })
	var known bool
	if i >= 0 {
		_, known = s.configs[i].Choice(args.Value)
	}
	worktree := s.rec.Worktree
	s.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, args.ID, args.Value)
	}
	if _, err := a.sendConfig(ctx, s, args.ID, args.Value); err != nil {
		return err
	}
	return a.host.StorageSet(ctx, configStorageKey(args.ID, worktree), args.Value)
}

func (a *app) sendConfig(ctx context.Context, s *session, id, value string) ([]acp.SessionConfigOption, error) {
	s.mu.Lock()
	p, agentID := s.proc, s.rec.AgentSessionID
	s.mu.Unlock()
	if p == nil {
		return nil, errNotLive
	}
	var res acp.SetSessionConfigOptionResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSetConfig, acp.SetSessionConfigOptionRequest{SessionID: agentID, ConfigID: id, Value: value}, &res); err != nil {
		return nil, fmt.Errorf("session/set_config_option: %w", err)
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindSetConfig, configPayload{ConfigID: id, Value: value, ConfigOptions: res.ConfigOptions})
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return res.ConfigOptions, nil
}

func (a *app) setAdvisor(ctx context.Context, s *session, value string) error {
	if !slices.ContainsFunc(advisorChoices, func(c advisorChoice) bool { return c.value == value }) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, advisorID, value)
	}
	s.mu.Lock()
	child, worktree := s.rec.ParentSession != "", s.rec.Worktree
	if child {
		s.rec.Advisor = value
	}
	s.mu.Unlock()
	if child {
		s.changed(changeUpdate, true)
		return nil
	}
	if err := a.host.StorageSet(ctx, advisorStorageKey(worktree), value); err != nil {
		return err
	}
	a.uiMu.Lock()
	a.advisors[worktree] = value
	a.uiMu.Unlock()
	s.changed(changeUpdate, false)
	return nil
}

func (a *app) sessionAdvisor(ctx context.Context, rec record) string {
	if rec.ParentSession != "" {
		return rec.Advisor
	}
	return a.advisor(ctx, rec.Worktree)
}

func (a *app) advisor(ctx context.Context, worktree string) string {
	a.uiMu.Lock()
	value, ok := a.advisors[worktree]
	a.uiMu.Unlock()
	if ok {
		return value
	}
	if _, err := a.host.StorageGet(ctx, advisorStorageKey(worktree), &value); err != nil {
		return ""
	}
	a.uiMu.Lock()
	a.advisors[worktree] = value
	a.uiMu.Unlock()
	return value
}

func pickable(o acp.SessionConfigOption) bool {
	return o.Type == acp.ConfigSelect && o.Category != acp.CategoryMode
}

func modeStorageKey(worktree string) string {
	return modeKey + worktreeHash(worktree)
}

func configStorageKey(id, worktree string) string {
	return configKey + id + "/" + worktreeHash(worktree)
}

func advisorStorageKey(worktree string) string {
	return advisorKey + worktreeHash(worktree)
}

func worktreeHash(worktree string) string {
	sum := sha256.Sum256([]byte(worktree))
	return hex.EncodeToString(sum[:])
}

func (a *app) resume(s *session) {
	s.mu.Lock()
	if s.rec.Closed {
		s.rec.Closed = false
		s.setStatusLocked(statusIdle, "")
	}
	s.mu.Unlock()
	s.changed(changeStatus, true)
	go a.attachInBackground(s)
}

func (a *app) closeSession(ctx context.Context, s *session, remove bool) *agentProc {
	s.mu.Lock()
	p, agentID, running, rec := s.proc, s.rec.AgentSessionID, s.running, s.rec
	s.proc = nil
	s.running = false
	s.queue = nil
	s.cancelPermissionsLocked()
	if !s.rec.Closed {
		s.recordLocked(actorUser, kindClose, acp.SessionRequest{SessionID: agentID})
		s.rec.Closed = true
		s.setStatusLocked(statusClosed, "")
	}
	ended := s.turnLocked()
	s.mu.Unlock()
	if running {
		s.turned(ended)
	}
	s.changed(changeStatus, true)
	a.cancelSignIn(ctx, rec.Workspace, rec.ID)
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req := acp.SessionRequest{SessionID: agentID}
	if running {
		_ = p.conn.Notify(acp.MethodSessionCancel, req)
	}
	if p.caps.SessionCapabilities.Close != nil {
		_ = p.conn.Call(ctx, acp.MethodSessionClose, req, nil)
	}
	if remove && p.caps.SessionCapabilities.Delete != nil {
		_ = p.conn.Call(ctx, acp.MethodSessionDelete, req, nil)
	}
	p.unregister(agentID)
	a.release(p)
	return p
}

func (a *app) closeTree(ctx context.Context, s *session) {
	seen := map[string]bool{}
	var walk func(*session)
	walk = func(x *session) {
		id := x.snapshot().ID
		if seen[id] {
			return
		}
		seen[id] = true
		a.closeSession(ctx, x, false)
		a.spawnMu.Lock()
		children := a.children(id)
		a.spawnMu.Unlock()
		for _, c := range children {
			walk(c)
		}
	}
	walk(s)
}

func (a *app) deleteTree(ctx context.Context, s *session) error {
	seen := map[string]bool{}
	var walk func(*session) error
	walk = func(x *session) error {
		id := x.snapshot().ID
		if seen[id] {
			return nil
		}
		seen[id] = true
		a.closeSession(ctx, x, true)
		a.spawnMu.Lock()
		children := a.children(id)
		a.spawnMu.Unlock()
		var errs []error
		for _, c := range children {
			errs = append(errs, walk(c))
		}
		return errors.Join(append(errs, a.deleteSession(ctx, x, true))...)
	}
	return walk(s)
}

func (a *app) deleteSession(ctx context.Context, s *session, remote bool) error {
	a.closeSession(ctx, s, remote)
	a.mu.Lock()
	delete(a.sessions, s.rec.ID)
	a.mu.Unlock()
	s.mu.Lock()
	s.deleted = true
	rec := s.rec
	err := s.log.remove()
	s.mu.Unlock()
	a.notes.mark(s, changeDeleted, true)
	a.forgetSession(ctx, rec.Workspace, rec.ID)
	a.kick(rec.Workspace)
	refsCtx, cancel := context.WithTimeout(context.Background(), refsTimeout)
	defer cancel()
	return errors.Join(err, a.deleteRefs(refsCtx, rec))
}

func (a *app) deleteRefs(ctx context.Context, rec record) error {
	if _, err := os.Stat(rec.Worktree); err == nil {
		return deleteCheckpoints(ctx, rec.Worktree, rec.ID)
	}
	p, err := a.host.GetProject(ctx, rec.Workspace)
	if err != nil {
		return nil
	}
	var errs []error
	for _, repo := range p.Repos {
		errs = append(errs, deleteCheckpoints(ctx, repo, rec.ID))
	}
	return errors.Join(errs...)
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

func (a *app) stopRoot(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, stopRootWait)
	defer cancel()
	root = gitrepo.Canonical(root)
	inside := func(dir string) bool { return gitrepo.Within(gitrepo.Canonical(dir), root) }
	a.mu.Lock()
	var hit []*session
	for id, s := range a.sessions {
		if inside(s.snapshot().Worktree) {
			a.dropped[id] = true
			hit = append(hit, s)
		}
	}
	a.mu.Unlock()
	for _, s := range hit {
		a.closeSession(ctx, s, false)
	}
	a.mu.Lock()
	var procs []*agentProc
	for p := range a.live {
		if inside(p.root) {
			procs = append(procs, p)
		}
	}
	for k, slot := range a.procs {
		if slot.proc != nil && inside(slot.proc.root) {
			delete(a.procs, k)
		}
	}
	a.mu.Unlock()
	for _, p := range procs {
		go p.shutdown()
	}
	for _, p := range procs {
		select {
		case <-p.exited:
		case <-ctx.Done():
		}
	}
	var errs []error
	for _, s := range hit {
		rec := s.snapshot()
		errs = append(errs, deleteCheckpoints(context.WithoutCancel(ctx), rec.Worktree, rec.ID))
	}
	return errors.Join(errs...)
}

func (a *app) dropWorkspace(projectID string, match func(record) bool, remove bool) {
	ctx, cancel := context.WithTimeout(context.Background(), refsTimeout)
	defer cancel()
	a.mu.Lock()
	var hit []*session
	for id, s := range a.sessions {
		if !a.dropped[id] {
			hit = append(hit, s)
		}
	}
	a.mu.Unlock()
	for _, s := range hit {
		rec := s.snapshot()
		if rec.Workspace != projectID || (match != nil && !match(rec)) {
			continue
		}
		a.mu.Lock()
		a.dropped[rec.ID] = true
		a.mu.Unlock()
		var err error
		if remove {
			err = a.deleteSession(ctx, s, false)
		} else {
			a.closeSession(ctx, s, false)
			err = a.deleteRefs(ctx, rec)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: drop session:", err)
		}
	}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}
