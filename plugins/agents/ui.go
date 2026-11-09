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

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/internal/claudectx"
	"clubmate/plugins/internal/gitrepo"
)

const (
	sidebarSlot   = "agents.sidebar"
	mainSlot      = "agents.main"
	sectionID     = "agents"
	codeSlot      = "editor-nvim.sidebar"
	editorSection = "editor-nvim"

	renderEvery    = 100 * time.Millisecond
	tickEvery      = 30 * time.Second
	maxSidebarRows = 1000
	renderTimeout  = 10 * time.Second

	selectedKey  = "selected/"
	collapsedKey = "collapsed/"
	codeKey      = "code/"
)

type uiState struct {
	uiMu           sync.Mutex
	ui             map[string]*workspaceUI
	goneWorkspaces map[string]bool
	reposChecked   map[string]bool
	auth           map[string]authCommand
	signIns        map[string]string
	nodeProblem    string
	installing     bool
	resumable      map[string]bool
	steers         map[string]bool
	noResume       map[string]bool
	advisors       map[string]string
	present        map[string]bool
	sections       map[string]string
	watched        map[string][]string
	editorFiles    map[string]editorFile
}

const editorFileTopic = "editor-nvim.file"

type editorFile struct {
	Workspace string `json:"workspace"`
	Root      string `json:"root"`
	Path      string `json:"path"`
}

type workspaceUI struct {
	render     sync.Mutex
	sent       map[string][]byte
	throttle   *throttle
	tick       *ticker
	selected   string
	filter     string
	query      string
	closed     int
	restored   bool
	collapsed  map[string]bool
	folds      bool
	branch     bool
	sessions   map[string]*sessionUI
	focusAfter string
	code       map[string]string
	codeRead   bool
	folder     *folderSheet
	archive    *archiveSheet
	cleanup    *cleanupSheet
	repos      *reposSheet
}

type sessionUI struct {
	window      int
	expanded    map[string]bool
	attachments []pendingAttachment
	nextID      int
	gen         int
	signIn      string
	anchor      string
	anchorTurn  int
	anchorRev   int
	open        openDraft
	repos       reposChoice
	restoreNote string

	context         bool
	contextSelected string
	contextOpen     map[string]bool
	contextReader   *claudectx.Reader
}

func newUIState() uiState {
	return uiState{
		ui: map[string]*workspaceUI{}, goneWorkspaces: map[string]bool{}, reposChecked: map[string]bool{}, auth: map[string]authCommand{}, signIns: map[string]string{},
		resumable: map[string]bool{}, steers: map[string]bool{}, noResume: map[string]bool{}, advisors: map[string]string{},
		present: map[string]bool{}, sections: map[string]string{}, watched: map[string][]string{}, editorFiles: map[string]editorFile{},
	}
}

func (a *app) workspaceState(workspaceID string) *workspaceUI {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	p, ok := a.ui[workspaceID]
	if !ok {
		p = &workspaceUI{sent: map[string][]byte{}, collapsed: map[string]bool{}, sessions: map[string]*sessionUI{}}
		p.throttle = newThrottle(realClock{}, renderEvery, func() { a.renderLogged(workspaceID) })
		p.tick = newTicker(realClock{}, tickEvery, p.throttle.kick)
		a.ui[workspaceID] = p
	}
	return p
}

func (a *app) sessionState(workspaceID, sid string) *sessionUI {
	p := a.workspaceState(workspaceID)
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

func (a *app) kick(workspaceID string) {
	a.uiMu.Lock()
	gone := a.goneWorkspaces[workspaceID]
	a.uiMu.Unlock()
	if !gone {
		a.workspaceState(workspaceID).throttle.kick()
	}
}

func (a *app) forgetWorkspace(workspaceID string) {
	a.uiMu.Lock()
	a.goneWorkspaces[workspaceID] = true
	p := a.ui[workspaceID]
	delete(a.ui, workspaceID)
	delete(a.present, workspaceID)
	delete(a.sections, workspaceID)
	delete(a.watched, workspaceID)
	delete(a.editorFiles, workspaceID)
	a.uiMu.Unlock()
	a.reminders.mu.Lock()
	delete(a.reminders.byWS, workspaceID)
	a.reminders.mu.Unlock()
	a.runNeeds.mu.Lock()
	delete(a.runNeeds.byWS, workspaceID)
	a.runNeeds.mu.Unlock()
	if p != nil {
		p.throttle.stop()
		p.tick.stop()
	}
}

func (a *app) forgetSession(ctx context.Context, workspaceID, sid string) {
	a.cancelSignIn(ctx, workspaceID, sid)
	a.uiMu.Lock()
	if p := a.ui[workspaceID]; p != nil {
		delete(p.sessions, sid)
	}
	delete(a.noResume, sid)
	a.uiMu.Unlock()
}

func (a *app) renderAll(ctx context.Context) {
	workspaces, err := a.host.ListWorkspaces(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: list workspaces:", err)
		return
	}
	a.removeLeftoverTerminals(ctx, workspaces)
	for _, p := range workspaces {
		if err := a.renderSlots(ctx, p.ID, a.slots()); err != nil {
			fmt.Fprintln(os.Stderr, "agents: render:", err)
		}
	}
}

func (a *app) renderLogged(workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	if err := a.render(ctx, workspaceID); err != nil {
		fmt.Fprintln(os.Stderr, "agents: render:", err)
	}
}

func (a *app) render(ctx context.Context, workspaceID string) error {
	return a.renderSlots(ctx, workspaceID, a.shownSlots(workspaceID))
}

func (a *app) renderSlots(ctx context.Context, workspaceID string, slots []string) error {
	if len(slots) == 0 {
		return nil
	}
	p := a.workspaceState(workspaceID)
	p.render.Lock()
	defer p.render.Unlock()
	workspace, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if workspace.Home {
		for _, slot := range slots {
			if err := a.push(ctx, p, workspaceID, slot, sdui.Empty(errHome.Error())); err != nil {
				return err
			}
		}
		return nil
	}
	recs := a.list(workspaceID)
	children := childStatuses(recs)
	slices.Reverse(recs)
	recs = recs[:min(len(recs), maxSidebarRows)]
	selected, err := a.resolveSelected(ctx, workspaceID, recs)
	if err != nil {
		return err
	}
	rows := make([]sessionRow, 0, len(recs))
	needs := a.runNeeds.byRun(workspaceID)
	for _, rec := range recs {
		row := a.sessionRow(ctx, rec, children[rec.ID])
		if n, ok := needs[runID(rec)]; ok {
			row.RunNeed = &n
		}
		rows = append(rows, row)
		if turnActive(rec) {
			p.tick.arm()
		}
	}
	if err := a.restoreCollapsed(ctx, workspaceID); err != nil {
		return err
	}
	if slices.Contains(slots, sheetSlot) {
		if err := a.push(ctx, p, workspaceID, sheetSlot, a.sheetTree(workspaceID, p)); err != nil {
			return err
		}
	}
	a.uiMu.Lock()
	collapsed := maps.Clone(p.collapsed)
	branchOpen := p.branch
	filter := p.filter
	search := sidebarSearch{query: p.query}
	closedExtra := p.closed
	a.uiMu.Unlock()
	if search.query != "" {
		search.results = a.search(workspaceID, search.query)
	}
	home, _, _ := workRoot(workspace)
	if err := a.push(ctx, p, workspaceID, sidebarSlot, sidebarView(rows, selected, collapsed, time.Now(), home, branchOpen, filter, search, closedExtra)); err != nil {
		return err
	}
	var shown *screen
	if slices.Contains(slots, mainSlot) {
		main := sdui.Empty(pickSession)
		if filter == filterOpen {
			q := a.openQueueScreen(workspaceID, rows, selected)
			q.Now = time.Now()
			main = openQueueView(q)
		} else if s := a.session(selected); s != nil {
			sc := a.screen(ctx, workspaceID, s, children[selected])
			sc.Workspace, sc.Folders = workspace.Name, workspace
			sc.Crumb = a.repoCrumb(s)
			main, shown = mainView(sc), &sc
		}
		if err := a.push(ctx, p, workspaceID, mainSlot, main); err != nil {
			return err
		}
	}
	if !slices.Contains(slots, codeSlot) {
		return nil
	}
	return a.renderCode(ctx, p, workspace, recs, children, shown)
}

func (a *app) slots() []string {
	if a.codePanel {
		return []string{sidebarSlot, mainSlot, sheetSlot, codeSlot}
	}
	return []string{sidebarSlot, mainSlot, sheetSlot}
}

func (a *app) shownSlots(workspaceID string) []string {
	a.uiMu.Lock()
	present, section, watched := a.present[workspaceID], a.sections[workspaceID], a.watched[workspaceID]
	a.uiMu.Unlock()
	if !present {
		return nil
	}
	shown := func(id string) bool { return section == "" || section == id || slices.Contains(watched, id) }
	slots := []string{sidebarSlot, sheetSlot}
	if shown(sectionID) {
		slots = append(slots, mainSlot)
	}
	if a.codePanel && shown(editorSection) {
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
	var p contract.UISection
	if err := json.Unmarshal(ev.Payload, &p); err != nil || p.Workspace == "" {
		return
	}
	switch ev.Topic {
	case "ui.opened", "ui.idle":
		if err := a.setPresent(ctx, p.Workspace, ev.Topic == "ui.opened"); err != nil {
			fmt.Fprintln(os.Stderr, "agents: presence:", err)
		}
		if ev.Topic == "ui.opened" {
			go a.offerRepos(p.Workspace)
		}
	case "ui.section":
		a.setSection(p.Workspace, p.Section)
	}
	a.kick(p.Workspace)
}

func (a *app) setPresent(ctx context.Context, workspaceID string, on bool) error {
	if !on {
		a.uiMu.Lock()
		delete(a.present, workspaceID)
		a.uiMu.Unlock()
		return nil
	}
	section, err := a.host.SelectedSection(ctx, workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.goneWorkspaces[workspaceID] {
		return nil
	}
	a.present[workspaceID] = true
	if err == nil {
		a.sections[workspaceID] = section
	}
	return err
}

func (a *app) setWatched(workspaceID string, sections []string) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.goneWorkspaces[workspaceID] {
		return
	}
	if len(sections) == 0 {
		delete(a.watched, workspaceID)
		return
	}
	a.watched[workspaceID] = sections
}

func (a *app) setSection(workspaceID, section string) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !a.goneWorkspaces[workspaceID] {
		a.sections[workspaceID] = section
	}
}

func (a *app) renderCode(ctx context.Context, p *workspaceUI, workspace pluginsdk.Workspace, recs []record, children map[string]string, shown *screen) error {
	if !a.codePanel {
		return nil
	}
	root, _, err := workRoot(workspace)
	if err != nil {
		return a.push(ctx, p, workspace.ID, codeSlot, sdui.Empty(err.Error()))
	}
	bound, err := a.codeSession(ctx, workspace.ID, root)
	if err != nil {
		return err
	}
	canonical := gitrepo.Canonical(root)
	here := slices.DeleteFunc(slices.Clone(recs), func(r record) bool {
		return r.Worktree != root && gitrepo.Canonical(r.Worktree) != canonical
	})
	if a.session(bound) == nil || !slices.ContainsFunc(recs, func(r record) bool { return r.ID == bound }) {
		bound = defaultCodeSession(here)
	}
	sc := shown
	if sc == nil || sc.View.Session.ID != bound {
		sc = nil
		if s := a.session(bound); s != nil {
			v := a.screen(ctx, workspace.ID, s, children[bound])
			v.Workspace, v.Folders = workspace.Name, workspace
			sc = &v
		}
	}
	return a.push(ctx, p, workspace.ID, codeSlot, codeView(here, bound, sc, a.editorFile(workspace.ID, root)))
}

func (a *app) setEditorFile(f editorFile) bool {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.goneWorkspaces[f.Workspace] || a.editorFiles[f.Workspace] == f {
		return false
	}
	a.editorFiles[f.Workspace] = f
	return true
}

func (a *app) editorFile(workspaceID, root string) string {
	a.uiMu.Lock()
	f := a.editorFiles[workspaceID]
	a.uiMu.Unlock()
	if f.Path == "" || gitrepo.Canonical(f.Root) != gitrepo.Canonical(root) {
		return ""
	}
	return f.Path
}

func (a *app) sessionRow(ctx context.Context, rec record, children string) sessionRow {
	var branch string
	if rec.Night == "" {
		branch = a.branches.name(ctx, rec.Worktree)
	}
	row := sessionRow{Rec: rec, Notify: a.effectiveNotify(rec), Children: children, Branch: branchLabel(branch), Detached: detachedBranch(branch), Review: a.reviews.progress(rec.ID)}
	if s := a.session(rec.ID); s != nil {
		row.Asking = s.asking()
		row.Started = s.turnStart()
		row.Rec.Asks = s.asks()
		if sum := s.permissionSummary(); sum != nil {
			row.Since = sum.At
		}
	}
	if at, ok := a.reminders.at(rec.Workspace, rec.ID); ok && rec.Reminding {
		row.Activity, row.Started = reminderActivity(at), time.Time{}
	}
	return row
}

func (a *app) screen(ctx context.Context, workspaceID string, s *session, children string) screen {
	if s.claimChangesCheck() {
		go a.refreshChanges(s)
	}
	v := s.view(0)
	v.Session.AsksParent = a.asksParent(s, v.Session)
	v.Session = a.reminders.present(v.Session)
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
	st := a.sessionState(workspaceID, sid)
	parent, fromTitles := a.relatives(v)
	advisor := a.sessionAdvisor(ctx, v.Session)
	row := a.sessionRow(ctx, v.Session, children)
	ctxScreen := a.contextScreen(st, v.Session)
	current := s.currentTurn()
	open := s.openCard()
	kids := a.repoKids(v.Session)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.open.prune(open.Items)
	if st.anchorTurn != current {
		st.anchor, st.anchorTurn = "", 0
	}
	needsAuth, cannotResume := a.recoveryLocked(v.Session)
	sc := screen{
		View:        v,
		Pending:     pending,
		Window:      st.window,
		Anchor:      st.anchor,
		AnchorRev:   st.anchorRev,
		Expanded:    maps.Clone(st.expanded),
		Repos:       reposScreenData{Kids: kids, Choice: st.repos.clone()},
		RestoreNote: st.restoreNote,
		Attachments: slices.Clone(st.attachments),
		Queue:       queue,
		Composer:    sid + "/composer/" + strconv.Itoa(st.gen),
		Steers:      a.steers[v.Session.Agent],
		NeedsAuth:   needsAuth,
		Continue:    cannotResume,
		SignIn:      st.signIn,
		NodeProblem: a.nodeProblem,
		Installing:  a.installing && detached && !v.Session.closed(),
		Changes:     changes,
		Advisor:     advisor,
		Branch:      row.Branch,
		Parent:      parent,
		Children:    row.Children,
		Row:         row,
		Now:         time.Now(),
		FromTitles:  fromTitles,
		Context:     ctxScreen,
		Review:      row.Review,
		Open:        open,
		OpenDraft:   st.open.clone(),
	}
	sc.ReviewStates, sc.ReviewsOpen = a.reviews.cards()
	return sc
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

func (a *app) push(ctx context.Context, p *workspaceUI, workspaceID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if bytes.Equal(p.sent[slot], raw) {
		return nil
	}
	if _, err := a.host.RenderJSON(ctx, workspaceID, slot, raw); err != nil {
		return err
	}
	p.sent[slot] = raw
	return nil
}

func (a *app) codeSessions(ctx context.Context, workspaceID string) (map[string]string, error) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	read, code := p.codeRead, maps.Clone(p.code)
	a.uiMu.Unlock()
	if read {
		return code, nil
	}
	if _, err := a.host.StorageGet(ctx, codeKey+workspaceID, &code); err != nil {
		return nil, err
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.codeRead {
		p.code, p.codeRead = code, true
	}
	return maps.Clone(p.code), nil
}

func (a *app) codeSession(ctx context.Context, workspaceID, root string) (string, error) {
	code, err := a.codeSessions(ctx, workspaceID)
	return code[root], err
}

func (a *app) bindCode(ctx context.Context, workspaceID, sid string) error {
	workspace, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	root, _, err := workRoot(workspace)
	if err != nil {
		return err
	}
	code, err := a.codeSessions(ctx, workspaceID)
	if err != nil {
		return err
	}
	if code == nil {
		code = map[string]string{}
	}
	code[root] = sid
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.code = maps.Clone(code)
	a.uiMu.Unlock()
	return a.host.StorageSet(ctx, codeKey+workspaceID, code)
}

func (a *app) resolveSelected(ctx context.Context, workspaceID string, recs []record) (string, error) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	restored, selected := p.restored, p.selected
	a.uiMu.Unlock()
	if !restored {
		if _, err := a.host.StorageGet(ctx, selectedKey+workspaceID, &selected); err != nil {
			return "", err
		}
	}
	resolved := selected
	if !slices.ContainsFunc(recs, func(r record) bool { return r.ID == selected }) {
		resolved = ""
		if i := slices.IndexFunc(recs, func(r record) bool { return !r.restricted() }); i >= 0 {
			resolved = recs[i].ID
		}
	}
	a.uiMu.Lock()
	p.restored, p.selected = true, resolved
	a.uiMu.Unlock()
	if resolved == selected && restored {
		return resolved, nil
	}
	return resolved, a.host.StorageSet(ctx, selectedKey+workspaceID, resolved)
}

func (a *app) selectSession(ctx context.Context, workspaceID, sid string) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.restored, p.selected = true, sid
	a.uiMu.Unlock()
	if err := a.host.StorageSet(ctx, selectedKey+workspaceID, sid); err != nil {
		return err
	}
	open := map[string]bool{}
	seen := map[string]bool{sid: true}
	for s := a.session(sid); s != nil; {
		rec := s.snapshot()
		if rec.Run != nil {
			open[runGroupKey(a.runScope(rec), rec.Run.ID)] = false
		}
		parent := rec.ParentSession
		if parent == "" && rec.restricted() && !rec.closed() {
			open[serviceKey] = false
		}
		if parent == "" || seen[parent] {
			break
		}
		seen[parent] = true
		open[expandPrefix+parent] = false
		s = a.session(parent)
	}
	if s := a.session(sid); s != nil && s.snapshot().taskFinished() {
		if err := a.restoreCollapsed(ctx, workspaceID); err != nil {
			return err
		}
		a.uiMu.Lock()
		once := p.collapsed[finishedKey+sid]
		a.uiMu.Unlock()
		if !once {
			open[finishedKey+sid], open[expandPrefix+sid] = true, true
		}
	}
	return a.setCollapsed(ctx, workspaceID, open)
}

func (a *app) runScope(rec record) string {
	switch {
	case a.session(rec.ParentSession) != nil:
		return rec.ParentSession
	case rec.closed() && rec.ParentSession == "":
		return closedKey
	case rec.restricted() && rec.ParentSession == "":
		return serviceKey
	}
	return rec.Worktree
}

func (a *app) reveal(ctx context.Context, workspaceID, sid string) error {
	if err := a.selectSession(ctx, workspaceID, sid); err != nil {
		return err
	}
	a.setSection(workspaceID, sectionID)
	if err := a.render(ctx, workspaceID); err != nil {
		return err
	}
	return a.focusComposer(ctx, workspaceID, sid)
}

func (a *app) focusComposer(ctx context.Context, workspaceID, sid string) error {
	return a.host.FocusNode(ctx, workspaceID, sectionID, mainSlot, sid+"/composer")
}

func (a *app) anchorTurn(workspaceID string, s *session, turn int) error {
	v := s.view(turn)
	i := slices.IndexFunc(v.Entries, func(en *entry) bool { return en.Kind == entryUser && en.Turn == turn })
	if i < 0 {
		return fmt.Errorf("%w: %d", errNoTurn, turn)
	}
	sid := v.Session.ID
	st := a.sessionState(workspaceID, sid)
	current := s.currentTurn()
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.anchor, st.anchorTurn = sid+"/msg/"+entryUser+"/"+v.Entries[i].ID, current
	st.anchorRev++
	st.window = max(st.window, current-turn+1)
	return nil
}

func (a *app) restoreCollapsed(ctx context.Context, workspaceID string) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	folds := p.folds
	a.uiMu.Unlock()
	if folds {
		return nil
	}
	var stored map[string]bool
	if _, err := a.host.StorageGet(ctx, collapsedKey+workspaceID, &stored); err != nil {
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

func (a *app) setCollapsed(ctx context.Context, workspaceID string, keys map[string]bool) error {
	if err := a.restoreCollapsed(ctx, workspaceID); err != nil {
		return err
	}
	p := a.workspaceState(workspaceID)
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
	return a.host.StorageSet(ctx, collapsedKey+workspaceID, collapsed)
}

func (a *app) recoveryLocked(rec record) (needsAuth, cannotResume bool) {
	_, needsAuth = a.auth[rec.ID]
	resumable, known := a.resumable[rec.Agent]
	return needsAuth, rec.AgentSessionID != "" && (a.noResume[rec.ID] || known && !resumable)
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
	workspaces := slices.Collect(maps.Keys(a.ui))
	a.uiMu.Unlock()
	for _, id := range workspaces {
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
