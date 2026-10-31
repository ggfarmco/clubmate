package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	sidebarSlot   = "agents.sidebar"
	mainSlot      = "agents.main"
	sectionID     = "agents"
	codeSlot      = "editor-nvim.sidebar"
	editorSection = "editor-nvim"
	editorSlot    = "editor-nvim.main"

	renderEvery    = 100 * time.Millisecond
	tickEvery      = 30 * time.Second
	maxSidebarRows = 1000
	renderTimeout  = 10 * time.Second

	selectedKey  = "selected/"
	collapsedKey = "collapsed/"
	codeKey      = "code/"
)

type uiState struct {
	uiMu         sync.Mutex
	ui           map[string]*projectUI
	goneProjects map[string]bool
	auth         map[string]authCommand
	signIns      map[string]string
	nodeProblem  string
	installing   bool
	resumable    map[string]bool
	steers       map[string]bool
	noResume     map[string]bool
	advisors     map[string]string
	present      map[string]bool
	sections     map[string]string
}

type projectUI struct {
	render    sync.Mutex
	sent      map[string][]byte
	throttle  *throttle
	tick      *ticker
	selected  string
	row       string
	restored  bool
	collapsed map[string]bool
	folds     bool
	branch    bool
	sessions  map[string]*sessionUI
	code      map[string]string
	codeRead  bool
}

type presencePayload struct {
	ProjectID string `json:"projectId"`
	Section   string `json:"section"`
}

type sessionUI struct {
	window      int
	expanded    map[string]bool
	attachments []pendingAttachment
	nextID      int
	gen         int
	signIn      string
}

func newUIState() uiState {
	return uiState{
		ui: map[string]*projectUI{}, goneProjects: map[string]bool{}, auth: map[string]authCommand{}, signIns: map[string]string{},
		resumable: map[string]bool{}, steers: map[string]bool{}, noResume: map[string]bool{}, advisors: map[string]string{},
		present: map[string]bool{}, sections: map[string]string{},
	}
}

func (a *app) projectState(projectID string) *projectUI {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	p, ok := a.ui[projectID]
	if !ok {
		p = &projectUI{sent: map[string][]byte{}, collapsed: map[string]bool{}, sessions: map[string]*sessionUI{}}
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

func (v *sessionUI) showEarlier() {
	v.window = max(v.window, windowStep) + windowStep
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
	delete(a.present, projectID)
	delete(a.sections, projectID)
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

func (a *app) renderAll(ctx context.Context) {
	projects, err := a.host.ListProjects(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: list projects:", err)
		return
	}
	a.removeLeftoverTerminals(ctx, projects)
	for _, p := range projects {
		if err := a.renderSlots(ctx, p.ID, a.slots()); err != nil {
			fmt.Fprintln(os.Stderr, "agents: render:", err)
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

func (a *app) render(ctx context.Context, projectID string) error {
	return a.renderSlots(ctx, projectID, a.shownSlots(projectID))
}

func (a *app) renderSlots(ctx context.Context, projectID string, slots []string) error {
	if len(slots) == 0 {
		return nil
	}
	p := a.projectState(projectID)
	p.render.Lock()
	defer p.render.Unlock()
	project, err := a.host.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project.Home {
		for _, slot := range slots {
			if err := a.push(ctx, p, projectID, slot, sdui.Empty(errHome.Error())); err != nil {
				return err
			}
		}
		return nil
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
	if slices.Contains(slots, mainSlot) {
		main := sdui.Empty(pickSession)
		if s := a.session(selected); s != nil {
			main = mainView(a.screen(ctx, projectID, s))
		}
		if err := a.push(ctx, p, projectID, mainSlot, main); err != nil {
			return err
		}
	}
	if !slices.Contains(slots, codeSlot) {
		return nil
	}
	return a.renderCode(ctx, p, project, recs)
}

func (a *app) slots() []string {
	if a.codePanel {
		return []string{sidebarSlot, mainSlot, codeSlot}
	}
	return []string{sidebarSlot, mainSlot}
}

func (a *app) shownSlots(projectID string) []string {
	a.uiMu.Lock()
	present, section := a.present[projectID], a.sections[projectID]
	a.uiMu.Unlock()
	if !present {
		return nil
	}
	slots := []string{sidebarSlot}
	if section == "" || section == sectionID {
		slots = append(slots, mainSlot)
	}
	if a.codePanel && (section == "" || section == editorSection) {
		slots = append(slots, codeSlot)
	}
	return slots
}

func (a *app) loadPresence(ctx context.Context) error {
	present, err := a.host.Presence(ctx)
	if err != nil {
		return err
	}
	for _, id := range present {
		if err := a.setPresent(ctx, id, true); err != nil {
			fmt.Fprintln(os.Stderr, "agents: presence:", err)
		}
	}
	return nil
}

func (a *app) presenceEvent(ctx context.Context, ev pluginsdk.Event) {
	var p presencePayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil || p.ProjectID == "" {
		return
	}
	switch ev.Topic {
	case "ui.opened", "ui.idle":
		if err := a.setPresent(ctx, p.ProjectID, ev.Topic == "ui.opened"); err != nil {
			fmt.Fprintln(os.Stderr, "agents: presence:", err)
		}
	case "ui.section":
		a.setSection(p.ProjectID, p.Section)
	}
	a.kick(p.ProjectID)
}

func (a *app) setPresent(ctx context.Context, projectID string, on bool) error {
	if !on {
		a.uiMu.Lock()
		delete(a.present, projectID)
		a.uiMu.Unlock()
		return nil
	}
	section, err := a.host.SelectedSection(ctx, projectID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.goneProjects[projectID] {
		return nil
	}
	a.present[projectID] = true
	if err == nil {
		a.sections[projectID] = section
	}
	return err
}

func (a *app) setSection(projectID, section string) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !a.goneProjects[projectID] {
		a.sections[projectID] = section
	}
}

func (a *app) renderCode(ctx context.Context, p *projectUI, project pluginsdk.Project, recs []record) error {
	if !a.codePanel {
		return nil
	}
	root, _, err := workRoot(project)
	if err != nil {
		return a.push(ctx, p, project.ID, codeSlot, sdui.Empty(err.Error()))
	}
	bound, err := a.codeSession(ctx, project.ID, root)
	if err != nil {
		return err
	}
	canonical := gitrepo.Canonical(root)
	here := slices.DeleteFunc(slices.Clone(recs), func(r record) bool {
		return r.Worktree != root && gitrepo.Canonical(r.Worktree) != canonical
	})
	if a.session(bound) == nil || !slices.ContainsFunc(recs, func(r record) bool { return r.ID == bound }) {
		bound = ""
		if i := slices.IndexFunc(here, func(r record) bool { return !r.Closed }); i >= 0 {
			bound = here[i].ID
		}
	}
	var sc *screen
	if s := a.session(bound); s != nil {
		v := a.screen(ctx, project.ID, s)
		sc = &v
	}
	return a.push(ctx, p, project.ID, codeSlot, codeView(here, bound, sc))
}

func (a *app) screen(ctx context.Context, projectID string, s *session) screen {
	if s.claimChangesCheck() {
		go a.refreshChanges(s)
	}
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
	advisor := a.sessionAdvisor(ctx, v.Session)
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
		Advisor:     advisor,
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

func (a *app) push(ctx context.Context, p *projectUI, projectID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if bytes.Equal(p.sent[slot], raw) {
		return nil
	}
	if _, err := a.host.RenderJSON(ctx, projectID, slot, raw); err != nil {
		return err
	}
	p.sent[slot] = raw
	return nil
}

func (a *app) codeSessions(ctx context.Context, projectID string) (map[string]string, error) {
	p := a.projectState(projectID)
	a.uiMu.Lock()
	read, code := p.codeRead, maps.Clone(p.code)
	a.uiMu.Unlock()
	if read {
		return code, nil
	}
	if _, err := a.host.StorageGet(ctx, codeKey+projectID, &code); err != nil {
		return nil, err
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.codeRead {
		p.code, p.codeRead = code, true
	}
	return maps.Clone(p.code), nil
}

func (a *app) codeSession(ctx context.Context, projectID, root string) (string, error) {
	code, err := a.codeSessions(ctx, projectID)
	return code[root], err
}

func (a *app) bindCode(ctx context.Context, projectID, sid string) error {
	project, err := a.host.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	root, _, err := workRoot(project)
	if err != nil {
		return err
	}
	code, err := a.codeSessions(ctx, projectID)
	if err != nil {
		return err
	}
	if code == nil {
		code = map[string]string{}
	}
	code[root] = sid
	p := a.projectState(projectID)
	a.uiMu.Lock()
	p.code = maps.Clone(code)
	a.uiMu.Unlock()
	return a.host.StorageSet(ctx, codeKey+projectID, code)
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

func (a *app) reveal(ctx context.Context, projectID, sid string) error {
	if err := a.selectSession(ctx, projectID, sid); err != nil {
		return err
	}
	a.setSection(projectID, sectionID)
	if err := a.render(ctx, projectID); err != nil {
		return err
	}
	return a.host.Focus(ctx, projectID, sectionID, mainSlot)
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

func (a *app) setNoResume(sid string, on bool) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if on {
		a.noResume[sid] = true
		return
	}
	delete(a.noResume, sid)
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
