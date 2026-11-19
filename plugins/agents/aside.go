package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	asideMode = "plan"

	asideOpen     = "open"
	asideThinking = "thinking"
	asideAsks     = "asks"
	asideSent     = "sent"

	asideIcon       = "bubble.left.and.bubble.right"
	asidesKey       = "asides:"
	asidesLabel     = "Asides · %d"
	asidesFoldAt    = 3
	asidePrefix     = "Aside · "
	asideQuoteRunes = 40

	asideSeedText    = "You are an aside forked from this session to discuss %s. Don't continue the session's task, don't edit files, don't land or start sessions; answer the user."
	asideOptionText  = "option %q of the question %q"
	asideOnlyOption  = "option %q"
	asidePassage     = "this passage: %q"
	asideKeepText    = "You are now a standalone session in %s; you don't own the task of %q; don't land it."
	asideSummaryText = "Summarize what we found in this aside in at most 5 sentences, written to be sent back to the parent session. Plain text, no preamble."
	sendAnswer       = "answer"
	sendDraft        = "draft"
	actAsidePick     = "asidePick"
	actAsideAsk      = "asideAsk"
	actAsideQuote    = "asideQuote"
	pinnedNode       = "pinned"
	asideSystemText  = "When you need the user's answer or decision before you can go on, ask it with the AskUserQuestion tool instead of ending your turn with a question in your reply."
)

var (
	asideDenied = []string{"ExitPlanMode", "mcp__" + mcpName, "mcp__" + mcpName + "-dev"}

	errForkUnsupported = errors.New("fork_unsupported: this session can't have asides")
	errAsideOfAside    = errors.New("an aside can't have asides")
	asideDetails       = map[string]string{asideOpen: "open", asideThinking: "thinking…", asideAsks: "asks you", asideSent: "sent"}

	errNoPlanMode    = errors.New("the agent offers no plan mode, so it can't run an aside")
	errNotAside      = errors.New("the session is not an aside")
	errParentGone    = errors.New("the aside's session is closed or gone")
	errStaleQuestion = errors.New("the question is no longer pending")
	errPickOnly      = errors.New("this question can't be prefilled; send it as a draft")
	errNoQuestion    = errors.New("this aside is not about a question; send it as a draft")
	errSendTo        = errors.New("invalid_params: to must be answer or draft")
	errAsideBusy     = errors.New("the aside is busy")
	errAsideNoFork   = errors.New("the aside never forked, so there is nothing to keep; close it and ask again")
	errAsideMode     = errors.New("an aside stays in plan mode; keep it to change its mode")
	errAsideHandoff  = errors.New("an aside can't be handed off; keep it first")
	errAsideUser     = errors.New("only the user opens, sends or picks from an aside")
)

type asideAnchor struct {
	Elicitation string `json:"elicitation,omitempty"`
	Field       int    `json:"field,omitempty"`
	Open        string `json:"open,omitempty"`
	Option      string `json:"option,omitempty"`
	Question    string `json:"question,omitempty"`
	Message     string `json:"message,omitempty"`
	Quote       string `json:"quote,omitempty"`
}

type asideInfo struct {
	Anchor asideAnchor `json:"anchor"`
	Sent   bool        `json:"sent,omitempty"`
	Kept   bool        `json:"kept,omitempty"`
	Framed bool        `json:"framed,omitempty"`
}

type asideView struct {
	State  string      `json:"state"`
	Label  string      `json:"label"`
	Detail string      `json:"detail"`
	Anchor asideAnchor `json:"anchor"`
}

type asideArgs struct {
	Session string     `json:"session"`
	Anchor  []keyValue `json:"anchor"`
}

type keyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type asideResult struct {
	Session  string `json:"session"`
	Reopened bool   `json:"reopened"`
}

func (r record) isAside() bool {
	return r.Aside != nil && !r.Aside.Kept
}

func asideState(rec record) string {
	switch {
	case rec.Status == statusRunning:
		return asideThinking
	case rec.Status == statusWaiting:
		return asideAsks
	case rec.Aside != nil && rec.Aside.Sent:
		return asideSent
	}
	return asideOpen
}

func asideViewOf(rec record) *asideView {
	if !rec.isAside() {
		return nil
	}
	state := asideState(rec)
	return &asideView{State: state, Label: rec.Title, Detail: asideDetails[state], Anchor: rec.Aside.Anchor}
}

func (a *app) canAside(rec record) bool {
	if rec.isAside() || rec.closed() || rec.restricted() || rec.AgentSessionID == "" {
		return false
	}
	if s := a.session(rec.ID); s == nil || !s.forkable() {
		return false
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	fork, known := a.forks[rec.Agent]
	return fork || !known
}

func (s *session) forkable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.rec.Unprompted
}

func (a *app) forkSource(rec record) string {
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return ""
	}
	return parent.snapshot().AgentSessionID
}

func (a *app) asideCommand(ctx context.Context, cmd pluginsdk.CommandRun, args asideArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errAsideUser
	}
	parent, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	an := anchorOf(args.Anchor)
	if an.Question == "" && an.Option != "" {
		an.Question = parent.anchorQuestion(an)
	}
	s, reopened, err := a.newAside(ctx, parent, an, cmd.Caller)
	if err != nil {
		return nil, err
	}
	return asideResult{Session: s.snapshot().ID, Reopened: reopened}, nil
}

func anchorOf(pairs []keyValue) asideAnchor {
	var an asideAnchor
	for _, kv := range pairs {
		switch kv.Key {
		case "elicitation":
			an.Elicitation = kv.Value
		case "field":
			an.Field, _ = strconv.Atoi(kv.Value)
		case "open":
			an.Open = kv.Value
		case "option":
			an.Option = kv.Value
		case "question":
			an.Question = kv.Value
		case "message":
			an.Message = kv.Value
		case "quote":
			an.Quote = kv.Value
		}
	}
	return an
}

func (s *session) anchorQuestion(an asideAnchor) string {
	if an.Elicitation != "" {
		if el, ok := s.elicitation(an.Elicitation); ok && an.Field >= 0 && an.Field < len(el.Fields) {
			return cmp.Or(el.Fields[an.Field].Title, el.Message)
		}
		return ""
	}
	items := s.openList()
	if i := slices.IndexFunc(items, func(it openItem) bool { return it.ID == an.Open }); i >= 0 {
		return items[i].Question
	}
	return ""
}

func asideLabel(an asideAnchor) string {
	if an.Option != "" {
		return asidePrefix + an.Option
	}
	return asidePrefix + "“" + clipRunes(strings.Join(strings.Fields(an.Quote), " "), asideQuoteRunes) + "”"
}

func (a *app) newAside(ctx context.Context, parent *session, an asideAnchor, from string) (*session, bool, error) {
	prec := parent.snapshot()
	if prec.isAside() {
		return nil, false, errAsideOfAside
	}
	if !a.canAside(prec) {
		return nil, false, errForkUnsupported
	}
	a.asideMu.Lock()
	defer a.asideMu.Unlock()
	if prec = parent.snapshot(); prec.closed() {
		return nil, false, errClosed
	}
	for _, c := range a.asides(prec.ID) {
		if rec := c.snapshot(); rec.isAside() && !rec.closed() && rec.Aside.Anchor.same(an) {
			return c, true, nil
		}
	}
	rec := record{Workspace: prec.Workspace, Worktree: prec.Worktree, Agent: prec.Agent, Title: asideLabel(an), TitleSet: true, DisallowedTools: asideDenied, Aside: &asideInfo{Anchor: an}, StartedBy: from}
	rec.ParentSession, rec.Advisor = prec.ID, a.sessionAdvisor(ctx, prec)
	return a.createSession(rec, nil), false, nil
}

func asideSeed(an asideAnchor) string {
	subject := fmt.Sprintf(asidePassage, an.Quote)
	switch {
	case an.Option != "" && an.Question != "":
		subject = fmt.Sprintf(asideOptionText, an.Option, an.Question)
	case an.Option != "":
		subject = fmt.Sprintf(asideOnlyOption, an.Option)
	}
	return fmt.Sprintf(asideSeedText, subject)
}

func (s *session) takeFrameLocked(parentTitle string) string {
	if s.rec.Aside == nil || s.rec.Aside.Framed {
		return ""
	}
	info := *s.rec.Aside
	info.Framed = true
	s.rec.Aside = &info
	if info.Kept {
		return fmt.Sprintf(asideKeepText, s.rec.Worktree, parentTitle)
	}
	return asideSeed(info.Anchor)
}

func (a *app) frameParentTitle(rec record) string {
	if rec.Aside == nil || !rec.Aside.Kept {
		return ""
	}
	if parent := a.session(rec.ForkedFrom); parent != nil {
		return parent.snapshot().Title
	}
	return ""
}

func (an asideAnchor) same(other asideAnchor) bool {
	an.Question, other.Question = "", ""
	return an == other
}

func (a *app) enterAsideMode(ctx context.Context, s *session, p *agentProc, agentID string, modes *acp.SessionModeState) error {
	if modes != nil && modes.CurrentModeID == asideMode {
		return nil
	}
	err := errNoPlanMode
	if modes != nil && slices.ContainsFunc(modes.AvailableModes, func(m acp.SessionMode) bool { return m.ID == asideMode }) {
		err = a.sendMode(ctx, s, asideMode)
	}
	if err == nil {
		return nil
	}
	s.mu.Lock()
	s.proc = nil
	s.mu.Unlock()
	p.unregister(agentID)
	a.dropAttached(p, agentID)
	return err
}

type composerDraft struct {
	Text string `json:"text"`
	Rev  int64  `json:"rev"`
}

type elicitPrefill struct {
	Value string
	Note  string
}

type asideSendArgs struct {
	Session string `json:"session"`
	To      string `json:"to"`
	Text    string `json:"text"`
	Close   *bool  `json:"close"`
}

type asidePickArgs struct {
	Session string `json:"session"`
	Value   string `json:"value"`
}

func (a *app) asideOf(workspaceID, id string) (*session, record, *session, error) {
	s, err := a.lookup(workspaceID, id)
	if err != nil {
		return nil, record{}, nil, err
	}
	rec := s.snapshot()
	if !rec.isAside() {
		return nil, record{}, nil, errNotAside
	}
	parent := a.session(rec.ParentSession)
	if parent == nil || parent.snapshot().closed() {
		return nil, record{}, nil, errParentGone
	}
	return s, rec, parent, nil
}

func (a *app) asideSummaryCommand(_ context.Context, cmd pluginsdk.CommandRun, args sessionArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errAsideUser
	}
	if _, _, _, err := a.asideOf(cmd.WorkspaceID, args.Session); err != nil {
		return nil, err
	}
	return struct{}{}, a.prompt(cmd.WorkspaceID, promptArgs{Session: args.Session, Text: asideSummaryText})
}

func (a *app) asideSendCommand(ctx context.Context, cmd pluginsdk.CommandRun, args asideSendArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errAsideUser
	}
	s, rec, parent, err := a.asideOf(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	an := rec.Aside.Anchor
	switch args.To {
	case sendAnswer:
		err = a.prefill(parent, an, an.Option, args.Text)
	case sendDraft:
		parent.setDraft(draftText(an.Quote, args.Text))
	default:
		err = errSendTo
	}
	if err != nil {
		return nil, err
	}
	s.markSent()
	if args.Close == nil || *args.Close {
		a.closeTree(ctx, s)
	}
	a.kick(cmd.WorkspaceID)
	return struct{}{}, nil
}

func (a *app) asidePickCommand(_ context.Context, cmd pluginsdk.CommandRun, args asidePickArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errAsideUser
	}
	return struct{}{}, a.asidePick(cmd.WorkspaceID, args.Session, args.Value)
}

func (a *app) asidePick(workspaceID, id, value string) error {
	_, rec, parent, err := a.asideOf(workspaceID, id)
	if err != nil {
		return err
	}
	if err := a.prefill(parent, rec.Aside.Anchor, value, ""); err != nil {
		return err
	}
	a.kick(workspaceID)
	return nil
}

func (a *app) prefill(parent *session, an asideAnchor, value, note string) error {
	switch {
	case an.Elicitation != "":
		spill, err := parent.prefillElicitation(an.Elicitation, an.Field, value, note)
		if err == nil && spill != "" {
			parent.setDraft(spill)
		}
		return err
	case an.Open != "":
		return a.prefillOpen(parent, an.Open, value, note)
	}
	return errNoQuestion
}

func (s *session) prefillElicitation(entry string, field int, value, note string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, found := s.index[key(entryElicitation, entry)]
	perm := s.perms[entry]
	if !found || perm == nil || perm.elicit == nil || s.entries[i].Elicitation == nil {
		return "", errStaleQuestion
	}
	el := s.entries[i].Elicitation
	if answersOnPick(el.Fields) {
		return "", errPickOnly
	}
	if field < 0 || field >= len(el.Fields) || !isChoice(el.Fields[field]) {
		return "", errNotAnElicitation
	}
	f := el.Fields[field]
	o := slices.IndexFunc(f.Options, func(o elicitOption) bool { return o.Value == value || o.Label == value })
	if o < 0 {
		return "", fmt.Errorf("%w: %q", errBadAnswer, value)
	}
	if s.prefills == nil {
		s.prefills = map[string]map[int]elicitPrefill{}
	}
	if s.prefills[entry] == nil {
		s.prefills[entry] = map[int]elicitPrefill{}
	}
	p := s.prefills[entry][field]
	p.Value = f.Options[o].Value
	spill := ""
	switch {
	case note == "":
	case f.Type == fieldChoice && slices.ContainsFunc(el.Fields, func(c elicitField) bool { return c.Key == f.Key+customSuffix && c.Type == fieldText }):
		p.Note = note
	default:
		spill = note
	}
	s.prefills[entry][field] = p
	return spill, nil
}

func (s *session) prefillsFor() map[string]map[int]elicitPrefill {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]map[int]elicitPrefill, len(s.prefills))
	for k, v := range s.prefills {
		out[k] = maps.Clone(v)
	}
	return out
}

func (a *app) prefillOpen(parent *session, item, value, note string) error {
	items := parent.openList()
	i := slices.IndexFunc(items, func(it openItem) bool { return it.ID == item })
	if i < 0 {
		return errStaleQuestion
	}
	it := items[i]
	o := slices.IndexFunc(it.Options, func(o openOption) bool { return o.Label == value })
	if o < 0 && value != otherOption {
		return fmt.Errorf("%w: %q", errBadAnswer, value)
	}
	rec := parent.snapshot()
	st := a.sessionState(rec.Workspace, rec.ID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.open.init()
	ans := st.open.answers[it.ID]
	ans.Item = it.ID
	switch {
	case o < 0 && !it.MultiSelect:
		ans.Option = ""
	case o < 0:
	case !it.MultiSelect:
		ans.Option = it.Options[o].Label
	case !slices.Contains(ans.Options, it.Options[o].Label):
		ans.Options = append(slices.Clone(ans.Options), it.Options[o].Label)
	}
	if note != "" {
		ans.Other = note
		st.open.others[it.ID] = true
	}
	st.open.answers[it.ID] = ans
	st.open.cursor = it.ID
	if st.openPrefills == nil {
		st.openPrefills = map[string]elicitPrefill{}
	}
	pf := elicitPrefill{Note: note}
	if o >= 0 {
		pf.Value = it.Options[o].Label
	}
	st.openPrefills[it.ID] = pf
	return nil
}

func draftText(quote, text string) string {
	if quote == "" {
		return text
	}
	return "> " + strings.ReplaceAll(quote, "\n", "\n> ") + "\n\n" + text
}

func (s *session) setDraft(text string) {
	s.mu.Lock()
	if s.rec.ComposerDraft != nil {
		s.rec.DraftRev = max(s.rec.DraftRev, s.rec.ComposerDraft.Rev)
	}
	s.rec.DraftRev++
	s.rec.ComposerDraft = &composerDraft{Text: text, Rev: s.rec.DraftRev}
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (s *session) clearDraft() {
	s.mu.Lock()
	had := s.rec.ComposerDraft != nil
	s.rec.ComposerDraft = nil
	s.mu.Unlock()
	if had {
		s.changed(changeUpdate, true)
	}
}

func (s *session) markSent() {
	s.mu.Lock()
	if s.rec.Aside != nil && !s.rec.Aside.Sent {
		info := *s.rec.Aside
		info.Sent = true
		s.rec.Aside = &info
	}
	s.mu.Unlock()
	s.changed(changeStatus, true)
}

func (a *app) pinnedStrip(rec record) *sdui.Node {
	if !rec.isAside() || rec.Aside.Anchor.Elicitation == "" {
		return nil
	}
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return nil
	}
	an := rec.Aside.Anchor
	f, pf, ok := parent.pendingChoice(an.Elicitation, an.Field)
	if !ok {
		return nil
	}
	p := choiceProps(f, nil, false)
	p.Pinned = true
	markPrefill(&p, pf)
	pinStrip(&p, parent.snapshot().Title, an.Option, rec)
	n := sdui.WithAction(sdui.Choice(rec.ID+"/"+pinnedNode, p), actAsidePick)
	return &n
}

func (s *session) pendingChoice(entry string, field int) (elicitField, elicitPrefill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, found := s.index[key(entryElicitation, entry)]
	perm := s.perms[entry]
	if !found || perm == nil || perm.elicit == nil || s.entries[i].Elicitation == nil {
		return elicitField{}, elicitPrefill{}, false
	}
	fields := s.entries[i].Elicitation.Fields
	if field < 0 || field >= len(fields) || !isChoice(fields[field]) || answersOnPick(fields) {
		return elicitField{}, elicitPrefill{}, false
	}
	return fields[field], s.prefills[entry][field], true
}

func markPrefill(p *sdui.ChoiceProps, pf elicitPrefill) {
	for i := range p.Options {
		if p.Options[i].Value == pf.Value {
			p.Options[i].On = true
		}
	}
	if pf.Note != "" && p.Other != nil {
		p.Other.On, p.Other.Value = true, pf.Note
	}
}

type optionAsides struct {
	marks map[string]sdui.ChoiceAside
	ask   bool
}

func elicitAsideMark(entry string, field int, value string) string {
	return entry + "/" + strconv.Itoa(field) + "/" + value
}

func openAsideMark(item, label string) string {
	return openNode + "/" + item + "/" + label
}

func (o optionAsides) apply(p *sdui.ChoiceProps, mark func(value string) string) {
	if o.ask {
		p.AsideAction = actAsideAsk
	}
	for i := range p.Options {
		if m, ok := o.marks[mark(p.Options[i].Value)]; ok {
			p.Options[i].Aside = &m
		}
	}
	if m, ok := o.marks[mark(otherOption)]; ok && p.Other != nil {
		other := *p.Other
		other.Aside = &m
		p.Other = &other
	}
}

func (a *app) optionAsides(rec record) optionAsides {
	out := optionAsides{marks: map[string]sdui.ChoiceAside{}, ask: a.canAside(rec)}
	for _, s := range a.asides(rec.ID) {
		r := s.snapshot()
		an, mark := r.Aside.Anchor, sdui.ChoiceAside{ID: r.ID, State: asideState(r)}
		switch {
		case an.Elicitation != "":
			out.marks[elicitAsideMark(an.Elicitation, an.Field, an.Option)] = mark
		case an.Open != "":
			out.marks[openAsideMark(an.Open, an.Option)] = mark
		}
	}
	return out
}

func (a *app) askAside(ctx context.Context, s *session, node, value string) error {
	sid := s.snapshot().ID
	an, ok := s.optionAnchor(strings.TrimPrefix(node, sid+"/"), value)
	if !ok {
		return fmt.Errorf("%w: %s on %q", errUnknownAction, actAsideAsk, node)
	}
	return a.openAside(ctx, s.snapshot().Workspace, s, an)
}

func (s *session) optionAnchor(node, value string) (asideAnchor, bool) {
	if rest, ok := strings.CutPrefix(node, entryElicitation+"/"); ok {
		entry, f, _ := strings.Cut(rest, "/f/")
		field, err := strconv.Atoi(f)
		el, found := s.elicitation(entry)
		if err != nil || !found || field < 0 || field >= len(el.Fields) {
			return asideAnchor{}, false
		}
		question := cmp.Or(el.Fields[field].Title, el.Message)
		return asideAnchor{Elicitation: entry, Field: field, Option: value, Question: question}, true
	}
	if item, ok := strings.CutPrefix(node, openNode+"/"); ok {
		items := s.openList()
		if i := slices.IndexFunc(items, func(it openItem) bool { return it.ID == item }); i >= 0 {
			return asideAnchor{Open: item, Option: value, Question: items[i].Question}, true
		}
	}
	return asideAnchor{}, false
}

func (a *app) asideKeepCommand(_ context.Context, cmd pluginsdk.CommandRun, args sessionArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errAsideUser
	}
	s, rec, parent, err := a.asideOf(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	mode, _ := parent.settings()
	s.mu.Lock()
	if s.running || turnActive(s.rec) || !s.rec.isAside() {
		s.mu.Unlock()
		return nil, errAsideBusy
	}
	if s.rec.AgentSessionID == "" {
		s.mu.Unlock()
		return nil, errAsideNoFork
	}
	s.rec.Title = strings.TrimPrefix(s.rec.Title, asidePrefix)
	info := *s.rec.Aside
	info.Kept, info.Framed = true, false
	s.rec.Aside, s.rec.ForkedFrom, s.rec.ParentSession, s.rec.DisallowedTools = &info, rec.ParentSession, "", nil
	if mode != "" {
		s.rec.Mode = mode
		s.stampRecLocked()
	}
	p, agentID := s.proc, s.rec.AgentSessionID
	s.proc = nil
	s.mu.Unlock()
	s.changed(changeStatus, true)
	if p != nil {
		p.unregister(agentID)
		a.dropAttached(p, agentID)
	}
	if _, err := a.attach(s); err != nil {
		a.fail(s, err.Error())
		return nil, err
	}
	a.kick(cmd.WorkspaceID)
	return struct{}{}, nil
}
