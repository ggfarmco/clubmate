package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	asideSlot = "agents.aside"

	actAsideFull   = "asideFull"
	actAsideBeside = "asideBeside"
	actAsideParent = "asideParent"
	actAsideNext   = "asideNext"
	actAsideKeep   = "asideKeep"
	actAsideClose  = "asideClose"
	actAsideForked = "asideForked"

	asideFullKey   = "cmd+shift+\\"
	asideParentKey = "cmd+["
	asideNextKey   = "cmd+]"
	asideKeepKey   = "cmd+shift+k"
	asideCloseHint = "opt+cmd+delete"

	asideFullNode   = "asideFull"
	asideKeepNode   = "asideKeep"
	asideCloseNode  = "asideClose"
	asideNextNode   = "asideNext"
	asideForkedNode = "forked"
	asideFromNode   = "asideFrom"

	readOnlyBadge     = "read-only"
	asidePlaceholder  = "Ask aside…"
	asideFullLabel    = "Full page"
	asideBesideLabel  = "Beside"
	asideKeepLabel    = "Keep"
	asideCloseLabel   = "Close aside"
	asideNextLabel    = "Aside"
	asidesNextLabel   = "Asides · %d"
	forkedQuestion    = "Forked from the question · Claude read everything the session knew"
	forkedPassage     = "Forked from a passage · Claude read everything the session knew"
	forkedFromLabel   = "forked from "
	asideFullIcon     = "arrow.up.left.and.arrow.down.right"
	asideBesideIcon   = "arrow.down.right.and.arrow.up.left"
	asideKeepIcon     = "tray.and.arrow.down"
	asideCloseIcon    = "xmark"
	asideParentIcon   = "arrow.turn.up.left"
	asideForkedIcon   = "arrow.branch"
	asideOptionPrefix = "Option · "
	pinnedAsks        = " asks"
	otherOption       = "Other"
	pinnedHint        = "Pick here: it answers the question in the parent's card, where Send still waits."
)

func pinStrip(p *sdui.ChoiceProps, parent, option string, rec record) {
	p.Note = cmp.Or(parent, "New session") + pinnedAsks
	p.Quote = pinnedHint
	for i := range p.Options {
		if p.Options[i].Value == option {
			p.Options[i].Aside = &sdui.ChoiceAside{ID: rec.ID, State: asideState(rec)}
		}
	}
}

type asideFocus struct {
	slot string
	node string
}

func (a *app) besideOf(workspaceID, selected string) string {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	id := p.beside[selected]
	a.uiMu.Unlock()
	if id == "" {
		return ""
	}
	s := a.session(id)
	if s == nil {
		return ""
	}
	if rec := s.snapshot(); !rec.isAside() || rec.closed() || rec.ParentSession != selected {
		return ""
	}
	return id
}

func (a *app) setBeside(workspaceID, parent, aside string) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.beside == nil {
		p.beside = map[string]string{}
	}
	if aside == "" {
		delete(p.beside, parent)
		return
	}
	p.beside[parent] = aside
}

func (a *app) focusAfterNode(workspaceID, slot, node string) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.focusNode = asideFocus{slot: slot, node: node}
	a.uiMu.Unlock()
}

func (a *app) takeFocusNode(workspaceID string) asideFocus {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	f := p.focusNode
	p.focusNode = asideFocus{}
	return f
}

func (a *app) asideScreen(ctx context.Context, workspaceID string, id string, children string) (screen, bool) {
	s := a.session(id)
	if s == nil {
		return screen{}, false
	}
	sc := a.screen(ctx, workspaceID, s, children)
	sc.Beside, sc.Pinned = true, nil
	return sc, true
}

func (a *app) showBeside(ctx context.Context, workspaceID, parent, aside string) error {
	a.setBeside(workspaceID, parent, aside)
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	if p.filter == filterOpen {
		p.filter = filterAll
	}
	selected := p.selected
	a.uiMu.Unlock()
	if selected != parent {
		if err := a.selectSession(ctx, workspaceID, parent); err != nil {
			return err
		}
	}
	a.focusAfterNode(workspaceID, asideSlot, aside+"/composer")
	return nil
}

func (a *app) openAside(ctx context.Context, workspaceID string, parent *session, an asideAnchor) error {
	s, _, err := a.newAside(ctx, parent, an, "")
	if err != nil {
		return err
	}
	return a.showBeside(ctx, workspaceID, parent.snapshot().ID, s.snapshot().ID)
}

type quoteValue struct {
	Quote string `json:"quote"`
}

func (a *app) askQuote(ctx context.Context, ev pluginsdk.UIEvent, s *session, node string) error {
	entry, ok := strings.CutPrefix(node, "msg/"+entryAgent+"/")
	if !ok {
		return fmt.Errorf("%w: %s on %q", errUnknownAction, actAsideQuote, node)
	}
	var v quoteValue
	if err := decode(ev.Value, &v); err != nil {
		return err
	}
	quote := strings.TrimSpace(v.Quote)
	if quote == "" {
		return fmt.Errorf("%w: an empty passage", errUnknownAction)
	}
	return a.openAside(ctx, ev.WorkspaceID, s, asideAnchor{Message: entry, Quote: quote})
}

func (a *app) askArmed(ctx context.Context, workspaceID string, s *session, item openItem, combo string) error {
	option := ""
	switch combo {
	case "1", "2", "3", "4":
		if i := int(combo[0] - '1'); i < len(item.Options) {
			option = item.Options[i].Label
		}
	case "o":
		option = otherOption
	}
	if option == "" {
		return nil
	}
	return a.openAside(ctx, workspaceID, s, asideAnchor{Open: item.ID, Option: option, Question: item.Question})
}

func (a *app) asideAct(ctx context.Context, ev pluginsdk.UIEvent, s *session) error {
	rec := s.snapshot()
	switch ev.Action {
	case actAsideNext:
		return a.nextAside(ctx, ev.WorkspaceID, rec.ID)
	case actAsideForked:
		if a.session(rec.ForkedFrom) == nil {
			return nil
		}
		return a.selectSession(ctx, ev.WorkspaceID, rec.ForkedFrom)
	}
	if !rec.isAside() {
		return nil
	}
	parent := rec.ParentSession
	switch ev.Action {
	case actAsideFull:
		a.setBeside(ev.WorkspaceID, parent, "")
		if err := a.selectSession(ctx, ev.WorkspaceID, rec.ID); err != nil {
			return err
		}
		a.focusAfterNode(ev.WorkspaceID, mainSlot, rec.ID+"/composer")
	case actAsideBeside:
		return a.showBeside(ctx, ev.WorkspaceID, parent, rec.ID)
	case actAsideParent:
		a.setBeside(ev.WorkspaceID, parent, rec.ID)
		if err := a.selectSession(ctx, ev.WorkspaceID, parent); err != nil {
			return err
		}
		a.anchorAside(ev.WorkspaceID, rec)
		a.focusAfterNode(ev.WorkspaceID, mainSlot, parent+"/composer")
	case actAsideKeep:
		if err := a.keepAside(ctx, s); err != nil {
			return err
		}
		a.setBeside(ev.WorkspaceID, parent, "")
		if err := a.selectSession(ctx, ev.WorkspaceID, rec.ID); err != nil {
			return err
		}
		a.focusAfterNode(ev.WorkspaceID, mainSlot, rec.ID+"/composer")
	case actAsideClose:
		a.closeTree(ctx, s)
		a.setBeside(ev.WorkspaceID, parent, "")
		if err := a.selectSession(ctx, ev.WorkspaceID, parent); err != nil {
			return err
		}
		a.focusAfterNode(ev.WorkspaceID, mainSlot, parent+"/composer")
	}
	return nil
}

func (a *app) nextAside(ctx context.Context, workspaceID, parent string) error {
	var ids []string
	for _, c := range a.asides(parent) {
		if rec := c.snapshot(); !rec.closed() {
			ids = append(ids, rec.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	next := ids[0]
	if i := slices.Index(ids, a.besideOf(workspaceID, parent)); i >= 0 {
		next = ids[(i+1)%len(ids)]
	}
	return a.showBeside(ctx, workspaceID, parent, next)
}

func (a *app) anchorAside(workspaceID string, rec record) {
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return
	}
	pid := rec.ParentSession
	an := rec.Aside.Anchor
	node := ""
	switch {
	case an.Elicitation != "":
		node = pid + "/" + entryElicitation + "/" + an.Elicitation + "/f/" + strconv.Itoa(an.Field)
	case an.Open != "":
		node = openItemNode(pid, an.Open)
	case an.Message != "":
		node = pid + "/msg/" + entryAgent + "/" + an.Message
	}
	if node == "" {
		return
	}
	st := a.sessionState(workspaceID, pid)
	current := parent.currentTurn()
	a.uiMu.Lock()
	st.anchor, st.anchorTurn = node, current
	st.anchorRev++
	a.uiMu.Unlock()
}

func (a *app) keepAside(ctx context.Context, s *session) error {
	rec := s.snapshot()
	_, err := a.asideKeepCommand(ctx, pluginsdk.CommandRun{WorkspaceID: rec.Workspace}, sessionArgs{Session: rec.ID})
	return err
}

func (a *app) liveAsides(parent string) int {
	n := 0
	for _, c := range a.asides(parent) {
		if !c.snapshot().closed() {
			n++
		}
	}
	return n
}

func asideHeader(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	display := displayStatus(rec, sc.Children)
	badges := []sdui.HeaderBadge{{Text: readOnlyBadge}}
	if needsYou(rec) {
		badges = append([]sdui.HeaderBadge{{Text: waitingBadge, Tone: statusTones[statusWaiting]}}, badges...)
	}
	var buttons []sdui.Node
	if turnActive(rec) {
		buttons = append(buttons, sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/stop", "Stop", "stop.fill"), actStop), sdui.VariantGhost))
	}
	buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+sendBackButtonNode, sendBackLabel, sendBackIcon), actSendBackOpen), sdui.VariantPrimary), sendBackKey))
	if sc.Beside {
		buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideFullNode, asideFullLabel, asideFullIcon), actAsideFull), sdui.VariantGhost), asideFullKey))
	} else {
		buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideFullNode, asideBesideLabel, asideBesideIcon), actAsideBeside), sdui.VariantGhost), asideFullKey))
	}
	buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideKeepNode, asideKeepLabel, asideKeepIcon), actAsideKeep), sdui.VariantGhost), asideKeepKey))
	if sc.Parent != "" {
		buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/parent", "from "+sc.Parent, asideParentIcon), actAsideParent), sdui.VariantGhost), asideParentKey))
	}
	closeButton := sdui.WithHint(sdui.WithIconOnly(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideCloseNode, asideCloseLabel, asideCloseIcon), actAsideClose), sdui.VariantDanger)), asideCloseHint)
	if sc.CloseAsk != "" {
		closeButton = sdui.WithConfirm(closeButton, sc.CloseAsk)
	}
	buttons = append(buttons, closeButton)
	return sdui.Header(cmp.Or(rec.Title, asidePrefix), sdui.HeaderProps{
		Icon:   asideIcon,
		Badges: badges,
		Status: display,
		Agent:  agentLabel(rec.Agent),
		Mode:   sessionModeLabel(sc.View),
	}, buttons...)
}

func forkedBlock(rec record) sdui.Node {
	an := rec.Aside.Anchor
	id := rec.ID + "/" + asideForkedNode
	if an.Option == "" {
		return sdui.WithID(sdui.Card("", asideIcon, sdui.MutedText(forkedPassage), sdui.Text("“"+an.Quote+"”")), id)
	}
	rows := []sdui.Node{sdui.MutedText(forkedQuestion)}
	if an.Question != "" {
		rows = append(rows, sdui.Text(an.Question))
	}
	rows = append(rows, sdui.MutedText(asideOptionPrefix+an.Option))
	return sdui.WithID(sdui.Card("", asideIcon, rows...), id)
}

func asideNextButton(sid string, n int) sdui.Node {
	label := asideNextLabel
	if n > 1 {
		label = fmt.Sprintf(asidesNextLabel, n)
	}
	return sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideNextNode, label, asideIcon), actAsideNext), sdui.VariantGhost), asideNextKey)
}

func forkedFromButton(sid, title string) sdui.Node {
	return sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+asideForkedNode, forkedFromLabel+title, asideForkedIcon), actAsideForked), sdui.VariantGhost)
}
