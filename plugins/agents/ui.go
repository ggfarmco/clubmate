package main

import (
	"cmp"
	"context"
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
)

const (
	renderEvery     = 100 * time.Millisecond
	maxSidebarRows  = 1000
	renderTimeout   = 10 * time.Second
	selectedKey     = "selected/"
	selectionCmd    = "editor-nvim.selection"
	openCmd         = "editor.open"
	reviewCmd       = "git.review"
	signInTitle     = "Sign in to "
	checkpointLabel = "before turn %d"
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
	throttle  *throttle
	selected  string
	restored  bool
	collapsed map[string]bool
	sessions  map[string]*sessionUI
}

type throttle struct {
	mu    sync.Mutex
	every time.Duration
	last  time.Time
	timer *time.Timer
	fn    func()
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

type reviewArgs struct {
	Base      string `json:"base"`
	Head      string `json:"head"`
	BaseLabel string `json:"baseLabel"`
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

func newThrottle(every time.Duration, fn func()) *throttle {
	return &throttle{every: every, fn: fn}
}

func (t *throttle) kick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		return
	}
	t.timer = time.AfterFunc(max(t.every-time.Since(t.last), 0), t.fire)
}

func (t *throttle) fire() {
	t.mu.Lock()
	t.timer = nil
	t.last = time.Now()
	t.mu.Unlock()
	t.fn()
}

func (v *sessionUI) showEarlier() {
	v.window = max(v.window, windowStep) + windowStep
}

func (a *app) projectState(projectID string) *projectUI {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	p, ok := a.ui[projectID]
	if !ok {
		p = &projectUI{collapsed: map[string]bool{}, sessions: map[string]*sessionUI{}}
		p.throttle = newThrottle(renderEvery, func() { a.renderLogged(projectID) })
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
	a.projectState(projectID).throttle.kick()
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
		if _, err := a.host.Render(ctx, projectID, sidebarSlot, sdui.Empty(errHome.Error())); err != nil {
			return err
		}
		_, err := a.host.Render(ctx, projectID, mainSlot, sdui.Empty(errHome.Error()))
		return err
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
		row := sessionRow{Rec: rec}
		if s := a.session(rec.ID); s != nil {
			row.Activity = s.activity()
		}
		rows = append(rows, row)
	}
	a.uiMu.Lock()
	collapsed := maps.Clone(p.collapsed)
	a.uiMu.Unlock()
	if _, err := a.host.Render(ctx, projectID, sidebarSlot, sidebarView(rows, selected, collapsed, time.Now())); err != nil {
		return err
	}
	main := sdui.Empty(pickSession)
	if s := a.session(selected); s != nil {
		main = mainView(a.screen(projectID, s))
	}
	_, err = a.host.Render(ctx, projectID, mainSlot, main)
	return err
}

func (a *app) screen(projectID string, s *session) screen {
	v := s.view(0)
	pending := s.pendingIDs()
	sid := v.Session.ID
	st := a.sessionState(projectID, sid)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	_, needsAuth := a.auth[sid]
	return screen{
		View:        v,
		Pending:     pending,
		Window:      st.window,
		Expanded:    maps.Clone(st.expanded),
		Attachments: slices.Clone(st.attachments),
		Composer:    sid + "/composer/" + strconv.Itoa(st.gen),
		NeedsAuth:   needsAuth,
		SignIn:      st.signIn,
		NodeProblem: a.nodeProblem,
	}
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
	p.restored, p.selected = true, sid
	a.uiMu.Unlock()
	return a.host.StorageSet(ctx, selectedKey+projectID, sid)
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
	if err == nil && ev.Action == actNew {
		err = h.Focus(ctx, ev.ProjectID, sectionID, mainSlot)
	}
	return nil, err
}

func (a *app) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case actNew:
		res, err := a.newSession(ctx, a.host, ev.ProjectID, agentClaude)
		if err != nil {
			return err
		}
		return a.selectSession(ctx, ev.ProjectID, res.Session)
	case actSelect:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("select value: %w", err)
		}
		if strings.HasPrefix(id, groupPrefix) {
			return nil
		}
		if _, err := a.lookup(ev.ProjectID, id); err != nil {
			return err
		}
		return a.selectSession(ctx, ev.ProjectID, id)
	case actExpand, actCollapse:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		p := a.projectState(ev.ProjectID)
		a.uiMu.Lock()
		p.collapsed[strings.TrimPrefix(id, groupPrefix)] = ev.Action == actCollapse
		a.uiMu.Unlock()
		return nil
	case actClose, actDelete:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		s, err := a.lookup(ev.ProjectID, id)
		if err != nil {
			return err
		}
		if ev.Action == actDelete {
			p := a.projectState(ev.ProjectID)
			a.uiMu.Lock()
			delete(p.sessions, id)
			a.uiMu.Unlock()
			return a.deleteSession(ctx, s)
		}
		a.closeSession(ctx, s, false)
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
		return a.host.RunCommand(ctx, openCmd, ev.ProjectID, v, nil)
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
	case actSignIn:
		return a.signIn(ctx, ev.ProjectID, s, st)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
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
		turn = firstCheckpoint(v.Turns)
	}
	i := slices.IndexFunc(v.Turns, func(t turnInfo) bool { return t.Turn == turn && t.CheckpointSha != "" })
	if i < 0 {
		return errNoCheckpoint
	}
	args := reviewArgs{Base: "commit:" + v.Turns[i].CheckpointSha, Head: v.Session.Worktree, BaseLabel: fmt.Sprintf(checkpointLabel, turn)}
	return a.host.RunCommand(ctx, reviewCmd, projectID, args, nil)
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
	if err := a.host.RemoveSession(ctx, p.ID); err != nil {
		fmt.Fprintln(os.Stderr, "agents: remove sign-in terminal:", err)
	}
	if s := a.session(sid); s != nil {
		a.resume(s)
	}
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
