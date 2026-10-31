package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	renderEvery    = 100 * time.Millisecond
	tickEvery      = 30 * time.Second
	maxSidebarRows = 1000
	renderTimeout  = 10 * time.Second
	selectedKey    = "selected/"
	collapsedKey   = "collapsed/"
	selectionCmd   = "editor-nvim.selection"
	openCmd        = "editor.open"
	editorSection  = "editor-nvim"
	editorSlot     = "editor-nvim.main"
	diffviewCmd    = "editor-nvim.diffview"
	signInTitle    = "Sign in to "
)

var (
	errUnknownAction = errors.New("unknown action")
	errNoCheckpoint  = errors.New("no checkpoint to review yet")
	errNoSignIn      = errors.New("the agent did not ask to sign in")
	errSignIn        = errors.New("sign in to continue")
	errNoSelection   = errors.New("no selection in the editor")
)

type authCommand struct {
	argv  []string
	env   []string
	label string
}

type sessionUI struct {
	window      int
	expanded    map[string]bool
	attachments []pendingAttachment
	nextID      int
	gen         int
	signIn      string
}

type projectUI struct {
	render    sync.Mutex
	sent      map[string][sha256.Size]byte
	throttle  *throttle
	tick      *ticker
	selected  string
	row       string
	restored  bool
	collapsed map[string]bool
	folds     bool
	branch    bool
	sessions  map[string]*sessionUI
}

type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) func() bool
}

type realClock struct{}

type throttle struct {
	mu      sync.Mutex
	clock   clock
	every   time.Duration
	last    time.Time
	pending func() bool
	stopped bool
	fn      func()
}

type ticker struct {
	mu      sync.Mutex
	clock   clock
	every   time.Duration
	stopAt  func() bool
	stopped bool
	fn      func()
}

type textValue struct {
	Text string `json:"text"`
}

type permissionValue struct {
	Request string `json:"request"`
	Option  string `json:"option"`
}

type modeValue struct {
	Mode string `json:"mode"`
}

type configValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type pathValue struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type idValue struct {
	ID string `json:"id"`
}

type turnValue struct {
	Turn int `json:"turn"`
}

type diffviewArgs struct {
	Cwd string `json:"cwd"`
	Rev string `json:"rev"`
}

type selection struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
}

type ptyEvent struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

func newThrottle(c clock, every time.Duration, fn func()) *throttle {
	return &throttle{clock: c, every: every, fn: fn}
}

func (t *throttle) kick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != nil || t.stopped {
		return
	}
	t.pending = t.clock.AfterFunc(max(t.every-t.clock.Now().Sub(t.last), 0), t.fire)
}

func (t *throttle) fire() {
	t.mu.Lock()
	t.pending = nil
	t.last = t.clock.Now()
	stopped := t.stopped
	t.mu.Unlock()
	if !stopped {
		t.fn()
	}
}

func newTicker(c clock, every time.Duration, fn func()) *ticker {
	return &ticker{clock: c, every: every, fn: fn}
}

func (t *ticker) arm() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopAt != nil || t.stopped {
		return
	}
	t.stopAt = t.clock.AfterFunc(t.every, t.fire)
}

func (t *ticker) fire() {
	t.mu.Lock()
	t.stopAt = nil
	t.mu.Unlock()
	t.fn()
}

func (t *ticker) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.stopAt != nil {
		t.stopAt()
		t.stopAt = nil
	}
}

func (t *throttle) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.pending != nil {
		t.pending()
		t.pending = nil
	}
}

func (v *sessionUI) showEarlier() {
	v.window = max(v.window, windowStep) + windowStep
}

func (a *app) projectState(projectID string) *projectUI {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	p, ok := a.ui[projectID]
	if !ok {
		p = &projectUI{sent: map[string][sha256.Size]byte{}, collapsed: map[string]bool{}, sessions: map[string]*sessionUI{}}
		p.throttle = newThrottle(realClock{}, renderEvery, func() { a.renderLogged(projectID) })
		p.tick = newTicker(realClock{}, tickEvery, p.throttle.kick)
		a.ui[projectID] = p
	}
	return p
}

func (a *app) sessionState(projectID, sid string) *sessionUI {
	p := a.projectState(projectID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	v, ok := p.sessions[sid]
	if !ok {
		v = &sessionUI{window: windowStep, expanded: map[string]bool{}}
		p.sessions[sid] = v
	}
	return v
}

func (a *app) kick(projectID string) {
	a.uiMu.Lock()
	gone := a.goneProjects[projectID]
	a.uiMu.Unlock()
	if !gone {
		a.projectState(projectID).throttle.kick()
	}
}

func (a *app) forgetProject(projectID string) {
	a.uiMu.Lock()
	a.goneProjects[projectID] = true
	p := a.ui[projectID]
	delete(a.ui, projectID)
	a.uiMu.Unlock()
	if p != nil {
		p.throttle.stop()
		p.tick.stop()
	}
}

func (a *app) forgetSession(ctx context.Context, projectID, sid string) {
	a.cancelSignIn(ctx, projectID, sid)
	a.uiMu.Lock()
	if p := a.ui[projectID]; p != nil {
		delete(p.sessions, sid)
	}
	delete(a.noResume, sid)
	a.uiMu.Unlock()
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

func (a *app) renderLogged(projectID string) {
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	if err := a.render(ctx, projectID); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render:", err)
	}
}

func (a *app) renderAll(ctx context.Context) {
	projects, err := a.host.ListProjects(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: list projects:", err)
		return
	}
	a.removeLeftoverTerminals(ctx, projects)
	for _, p := range projects {
		if err := a.render(ctx, p.ID); err != nil {
			fmt.Fprintln(os.Stderr, "agents: render:", err)
		}
	}
}

func (a *app) render(ctx context.Context, projectID string) error {
	p := a.projectState(projectID)
	p.render.Lock()
	defer p.render.Unlock()
	project, err := a.host.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project.Home {
		if err := a.push(ctx, p, projectID, sidebarSlot, sdui.Empty(errHome.Error())); err != nil {
			return err
		}
		return a.push(ctx, p, projectID, mainSlot, sdui.Empty(errHome.Error()))
	}
	recs := a.list(projectID)
	slices.Reverse(recs)
	recs = recs[:min(len(recs), maxSidebarRows)]
	selected, err := a.resolveSelected(ctx, projectID, recs)
	if err != nil {
		return err
	}
	rows := make([]sessionRow, 0, len(recs))
	for _, rec := range recs {
		row := sessionRow{Rec: rec, Notify: a.effectiveNotify(rec)}
		if s := a.session(rec.ID); s != nil {
			row.Activity = s.activity()
			row.Started = s.turnStart()
			row.Rec.Asks = s.asks()
		}
		rows = append(rows, row)
		if turnActive(rec) {
			p.tick.arm()
		}
	}
	if err := a.restoreCollapsed(ctx, projectID); err != nil {
		return err
	}
	a.uiMu.Lock()
	collapsed := maps.Clone(p.collapsed)
	branchOpen := p.branch
	row := selected
	if p.row == needsPrefix+selected {
		row = p.row
	}
	a.uiMu.Unlock()
	if err := a.push(ctx, p, projectID, sidebarSlot, sidebarView(rows, row, collapsed, time.Now(), branchOpen)); err != nil {
		return err
	}
	main := sdui.Empty(pickSession)
	if s := a.session(selected); s != nil {
		if s.claimChangesCheck() {
			go a.refreshChanges(s)
		}
		sc := a.screen(projectID, s)
		sc.Advisor = a.sessionAdvisor(ctx, sc.View.Session)
		main = mainView(sc)
	}
	return a.push(ctx, p, projectID, mainSlot, main)
}

func (a *app) push(ctx context.Context, p *projectUI, projectID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if p.sent[slot] == sum {
		return nil
	}
	if _, err := a.host.Render(ctx, projectID, slot, n); err != nil {
		return err
	}
	p.sent[slot] = sum
	return nil
}

func (a *app) screen(projectID string, s *session) screen {
	v := s.view(0)
	if a.session(v.Session.HandoffFrom) == nil {
		v.Session.HandoffFrom = ""
	}
	if a.session(v.Session.HandoffTo) == nil {
		v.Session.HandoffTo = ""
	}
	queue := s.queued()
	pending := s.pendingIDs()
	detached := s.detached()
	changes := s.hasChanges()
	sid := v.Session.ID
	st := a.sessionState(projectID, sid)
	parent, fromTitles := a.relatives(v)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	_, needsAuth := a.auth[sid]
	resumable, known := a.resumable[v.Session.Agent]
	cannotResume := v.Session.AgentSessionID != "" && (a.noResume[sid] || known && !resumable)
	return screen{
		View:        v,
		Pending:     pending,
		Window:      st.window,
		Expanded:    maps.Clone(st.expanded),
		Attachments: slices.Clone(st.attachments),
		Queue:       queue,
		Composer:    sid + "/composer/" + strconv.Itoa(st.gen),
		Steers:      a.steers[v.Session.Agent],
		NeedsAuth:   needsAuth,
		Continue:    cannotResume,
		SignIn:      st.signIn,
		NodeProblem: a.nodeProblem,
		Installing:  a.installing && detached && !v.Session.Closed,
		Changes:     changes,
		Parent:      parent,
		FromTitles:  fromTitles,
	}
}

func (a *app) relatives(v transcriptView) (string, map[string]string) {
	title := func(id string) (string, bool) {
		s := a.session(id)
		if s == nil {
			return "", false
		}
		return cmp.Or(s.snapshot().Title, "New session"), true
	}
	parent := ""
	if v.Session.ParentSession != "" {
		parent, _ = title(v.Session.ParentSession)
	}
	titles := map[string]string{}
	for _, en := range v.Entries {
		if _, done := titles[en.From]; en.From == "" || en.From == unknownCaller || done {
			continue
		}
		if t, ok := title(en.From); ok {
			titles[en.From] = t
		}
	}
	return parent, titles
}

func (a *app) resolveSelected(ctx context.Context, projectID string, recs []record) (string, error) {
	p := a.projectState(projectID)
	a.uiMu.Lock()
	restored, selected := p.restored, p.selected
	a.uiMu.Unlock()
	if !restored {
		if _, err := a.host.StorageGet(ctx, selectedKey+projectID, &selected); err != nil {
			return "", err
		}
	}
	resolved := selected
	if !slices.ContainsFunc(recs, func(r record) bool { return r.ID == selected }) {
		resolved = ""
		if len(recs) > 0 {
			resolved = recs[0].ID
		}
	}
	a.uiMu.Lock()
	p.restored, p.selected = true, resolved
	a.uiMu.Unlock()
	if resolved == selected && restored {
		return resolved, nil
	}
	return resolved, a.host.StorageSet(ctx, selectedKey+projectID, resolved)
}

func (a *app) selectSession(ctx context.Context, projectID, sid string) error {
	p := a.projectState(projectID)
	a.uiMu.Lock()
	p.restored, p.selected, p.row = true, sid, sid
	a.uiMu.Unlock()
	if err := a.host.StorageSet(ctx, selectedKey+projectID, sid); err != nil {
		return err
	}
	open := map[string]bool{}
	seen := map[string]bool{sid: true}
	for s := a.session(sid); s != nil; {
		parent := s.snapshot().ParentSession
		if parent == "" || seen[parent] {
			break
		}
		seen[parent] = true
		open[expandPrefix+parent] = false
		s = a.session(parent)
	}
	return a.setCollapsed(ctx, projectID, open)
}

func (a *app) restoreCollapsed(ctx context.Context, projectID string) error {
	p := a.projectState(projectID)
	a.uiMu.Lock()
	folds := p.folds
	a.uiMu.Unlock()
	if folds {
		return nil
	}
	var stored map[string]bool
	if _, err := a.host.StorageGet(ctx, collapsedKey+projectID, &stored); err != nil {
		return err
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.folds {
		maps.Copy(stored, p.collapsed)
		if stored != nil {
			p.collapsed = stored
		}
		p.folds = true
	}
	return nil
}

func (a *app) setCollapsed(ctx context.Context, projectID string, keys map[string]bool) error {
	if err := a.restoreCollapsed(ctx, projectID); err != nil {
		return err
	}
	p := a.projectState(projectID)
	a.uiMu.Lock()
	changed := false
	for k, v := range keys {
		if old, ok := p.collapsed[k]; !ok || old != v {
			p.collapsed[k], changed = v, true
		}
	}
	collapsed := maps.Clone(p.collapsed)
	a.uiMu.Unlock()
	if !changed {
		return nil
	}
	return a.host.StorageSet(ctx, collapsedKey+projectID, collapsed)
}

func (a *app) session(id string) *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[id]
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	select {
	case <-a.loaded:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	err := a.act(ctx, ev)
	if rerr := a.render(ctx, ev.ProjectID); err == nil {
		err = rerr
	}
	switch {
	case err != nil:
	case ev.Action == actNew || ev.Action == actNewBranch:
		err = h.Focus(ctx, ev.ProjectID, sectionID, mainSlot)
	case ev.Action == actShowBranch:
		err = h.FocusField(ctx, ev.ProjectID, sectionID)
	}
	return nil, err
}

func (a *app) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case actNew:
		res, err := a.newSession(ctx, a.host, ev.ProjectID, "", newArgs{})
		if err != nil {
			return err
		}
		return a.selectSession(ctx, ev.ProjectID, res.Session)
	case actShowBranch, actHideBranch:
		p := a.projectState(ev.ProjectID)
		a.uiMu.Lock()
		p.branch = ev.Action == actShowBranch
		a.uiMu.Unlock()
		return nil
	case actNewBranch:
		var args newArgs
		if err := decode(ev.Value, &args); err != nil {
			return err
		}
		branch := strings.TrimSpace(args.Branch)
		if branch == "" {
			return errNoBranch
		}
		res, err := a.newSession(ctx, a.host, ev.ProjectID, "", newArgs{Branch: branch})
		if err != nil {
			return err
		}
		p := a.projectState(ev.ProjectID)
		a.uiMu.Lock()
		p.branch = false
		a.uiMu.Unlock()
		return a.selectSession(ctx, ev.ProjectID, res.Session)
	case actSelect:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("select value: %w", err)
		}
		if strings.HasPrefix(id, groupPrefix) {
			return nil
		}
		sid := strings.TrimPrefix(id, needsPrefix)
		s, err := a.lookup(ev.ProjectID, sid)
		if err != nil {
			return err
		}
		s.invalidateChanges()
		if err := a.selectSession(ctx, ev.ProjectID, sid); err != nil {
			return err
		}
		p := a.projectState(ev.ProjectID)
		a.uiMu.Lock()
		p.row = id
		a.uiMu.Unlock()
		return nil
	case actExpand, actCollapse:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		key := expandPrefix + id
		if wt, ok := strings.CutPrefix(id, groupPrefix); ok {
			key = wt
		}
		return a.setCollapsed(ctx, ev.ProjectID, map[string]bool{key: ev.Action == actCollapse})
	case actClose, actDelete, actHandoff:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		s, err := a.lookup(ev.ProjectID, strings.TrimPrefix(id, needsPrefix))
		if err != nil {
			return err
		}
		switch ev.Action {
		case actDelete:
			return a.deleteTree(ctx, s)
		case actHandoff:
			return a.handoffAndSelect(ctx, ev.ProjectID, s)
		}
		a.closeTree(ctx, s)
		return nil
	case actNotifyAll, actNotifyNeeds, actNotifyMuted:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		s, err := a.lookup(ev.ProjectID, strings.TrimPrefix(id, needsPrefix))
		if err != nil {
			return err
		}
		i := slices.IndexFunc(notifyMenu, func(n notifyChoice) bool { return n.action == ev.Action })
		a.setNotify(s, notifyMenu[i].policy)
		return nil
	}
	sid, rest, _ := strings.Cut(ev.NodeID, "/")
	s, err := a.lookup(ev.ProjectID, sid)
	if err != nil {
		return err
	}
	return a.sessionAct(ctx, ev, s, rest)
}

func (a *app) sessionAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, node string) error {
	sid := s.snapshot().ID
	st := a.sessionState(ev.ProjectID, sid)
	switch ev.Action {
	case actSend:
		var v textValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.send(ev.ProjectID, s, st, v.Text)
	case actStop:
		a.cancel(s)
		return nil
	case actSelectParent:
		parent := s.snapshot().ParentSession
		if a.session(parent) == nil {
			return nil
		}
		return a.selectSession(ctx, ev.ProjectID, parent)
	case actPermission:
		var v permissionValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return s.answer(v.Request, v.Option)
	case actMode:
		var v modeValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setMode(ctx, ev.ProjectID, modeArgs{Session: sid, Mode: v.Mode})
	case actConfig:
		var v configValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setConfig(ctx, ev.ProjectID, configArgs{Session: sid, ID: v.ID, Value: v.Value})
	case actAttachFile:
		var v pathValue
		if err := decode(ev.Value, &v); err != nil || v.Path == "" {
			return errors.Join(err, errors.New("attachFile needs a path"))
		}
		a.addAttachment(st, attachment{Kind: "file", Path: v.Path})
		return nil
	case actAttachSelection:
		var sel selection
		if err := a.host.RunCommand(ctx, selectionCmd, ev.ProjectID, nil, &sel); err != nil {
			return err
		}
		if sel.Path == "" {
			return errNoSelection
		}
		a.addAttachment(st, attachment{Kind: "selection", Path: sel.Path, StartLine: sel.StartLine, EndLine: sel.EndLine, Text: sel.Text})
		return nil
	case actDetach:
		var v idValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		a.uiMu.Lock()
		st.attachments = slices.DeleteFunc(st.attachments, func(p pendingAttachment) bool { return p.ID == v.ID })
		a.uiMu.Unlock()
		return nil
	case actOpenLocation:
		var v pathValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		if err := a.host.RunCommand(ctx, openCmd, ev.ProjectID, v, nil); err != nil {
			return err
		}
		return a.host.Focus(ctx, ev.ProjectID, editorSection, editorSlot)
	case actReview:
		var v turnValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.review(ctx, ev.ProjectID, s, v.Turn)
	case actShowEarlier:
		a.uiMu.Lock()
		st.showEarlier()
		a.uiMu.Unlock()
		return nil
	case actShowAll:
		tool, ok := strings.CutPrefix(node, "tool/")
		if !ok {
			return fmt.Errorf("%w: showAll on %q", errUnknownAction, ev.NodeID)
		}
		a.uiMu.Lock()
		st.expanded[tool] = true
		a.uiMu.Unlock()
		return nil
	case actRetry:
		a.resume(s)
		return nil
	case actContinue:
		return a.handoffAndSelect(ctx, ev.ProjectID, s)
	case actOpenHandoff:
		rec := s.snapshot()
		target := rec.HandoffTo
		if node == handoffFromNode {
			target = rec.HandoffFrom
		}
		if a.session(target) == nil {
			return nil
		}
		return a.selectSession(ctx, ev.ProjectID, target)
	case actSignIn:
		return a.signIn(ctx, ev.ProjectID, s, st)
	case actUnqueue:
		id, ok := strings.CutPrefix(node, queuePrefix)
		if !ok {
			return fmt.Errorf("%w: unqueue on %q", errUnknownAction, ev.NodeID)
		}
		s.unqueue(id)
		return nil
	case actSendQueued:
		return a.submit(s, nil)
	case actMerge:
		return a.mergeToMain(ctx, ev.ProjectID, s)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) handoffAndSelect(ctx context.Context, projectID string, s *session) error {
	next, err := a.handoff(ctx, s, handoffArgs{}, "")
	if err != nil {
		return err
	}
	return a.selectSession(ctx, projectID, next.snapshot().ID)
}

func (a *app) addAttachment(st *sessionUI, att attachment) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.nextID++
	st.attachments = append(st.attachments, pendingAttachment{ID: "a" + strconv.Itoa(st.nextID), attachment: att})
}

func (a *app) send(projectID string, s *session, st *sessionUI, text string) error {
	a.uiMu.Lock()
	pending := st.attachments
	a.uiMu.Unlock()
	if strings.TrimSpace(text) == "" && len(pending) == 0 {
		return nil
	}
	atts := make([]attachment, 0, len(pending))
	for _, p := range pending {
		atts = append(atts, p.attachment)
	}
	if err := a.prompt(projectID, promptArgs{Session: s.snapshot().ID, Text: text, Attachments: atts}); err != nil {
		return err
	}
	a.uiMu.Lock()
	st.attachments = nil
	st.gen++
	a.uiMu.Unlock()
	return nil
}

func (a *app) review(ctx context.Context, projectID string, s *session, turn int) error {
	v := s.view(0)
	if turn == 0 {
		turn = firstCheckpoint(v.Turns).Turn
	}
	i := slices.IndexFunc(v.Turns, func(t turnInfo) bool { return t.Turn == turn && t.CheckpointSha != "" })
	if i < 0 {
		return errNoCheckpoint
	}
	args := diffviewArgs{Cwd: v.Session.Worktree, Rev: v.Turns[i].CheckpointSha}
	return a.host.RunCommand(ctx, diffviewCmd, projectID, args, nil)
}

func (a *app) refreshChanges(s *session) {
	s.changesMu.Lock()
	defer s.changesMu.Unlock()
	workspace, root, base := s.changesBase()
	changes := false
	if base != "" {
		ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
		defer cancel()
		var err error
		if changes, err = changedSince(ctx, root, base); err != nil {
			fmt.Fprintln(os.Stderr, "agents: changes:", err)
		}
	}
	s.mu.Lock()
	differs := s.changes != changes
	s.changes = changes
	s.mu.Unlock()
	if differs {
		a.kick(workspace)
	}
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

func (a *app) authRequired(sid string, p *agentProc, err error) error {
	if rpcErr, ok := errors.AsType[*acp.Error](err); !ok || rpcErr.Code != acp.CodeAuthRequired {
		return nil
	}
	return a.needsAuth(sid, p)
}

func (a *app) setNoResume(sid string, on bool) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if on {
		a.noResume[sid] = true
		return
	}
	delete(a.noResume, sid)
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

func (a *app) setInstalling(on bool) {
	a.uiMu.Lock()
	a.installing = on
	projects := slices.Collect(maps.Keys(a.ui))
	a.uiMu.Unlock()
	for _, id := range projects {
		a.kick(id)
	}
}

func (a *app) setNodeProblem(err error) {
	text := ""
	var nv nodeVersionError
	if errors.As(err, &nv) {
		text = nv.Error()
	}
	a.uiMu.Lock()
	a.nodeProblem = text
	a.uiMu.Unlock()
}

func (s *session) pendingIDs() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.perms))
	for id := range s.perms {
		out[id] = true
	}
	return out
}

func (s *session) hasChanges() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

func (s *session) claimChangesCheck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if s.changesFresh {
		return false
	}
	s.changesFresh = true
	return true
}

func (s *session) invalidateChanges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changesFresh = false
}

func (s *session) changesBase() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.rec.Workspace, s.rec.Worktree, firstCheckpoint(s.turns).CheckpointSha
}

func (s *session) detached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc == nil
}

func (s *session) turnStart() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *session) activity() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded || (s.rec.Status != statusRunning && s.rec.Status != statusWaiting) {
		return ""
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		en := s.entries[i]
		var title string
		switch {
		case s.rec.Status == statusWaiting && en.Kind == entryPermission && s.perms[en.ID] != nil:
			var f toolFields
			_ = json.Unmarshal(en.ToolCall, &f)
			title = f.Title
		case s.rec.Status == statusRunning && en.Kind == entryTool:
			_ = json.Unmarshal(en.Tool["title"], &title)
		default:
			continue
		}
		return firstLine(title)
	}
	return ""
}
