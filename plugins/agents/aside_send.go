package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	actSendBackOpen   = "sendBackOpen"
	actSendBackDraft  = "sendBackDraft"
	actSendBackFrom   = "sendBackFrom"
	actSendBackSend   = "sendBackSend"
	actSendBackCancel = "sendBackCancel"
	actSendBackKey    = "sendBackKey"

	sendBackNode       = "sendBack"
	sendBackButtonNode = "sendBackButton"
	sendBackKey        = "cmd+shift+return"
	sendBackLabel      = "Send back"
	sendBackIcon       = "arrow.uturn.backward"

	fromSummary = "summary"
	fromLast    = "last"

	sendBackTitle     = "Send back to %s"
	sendBackPicks     = "Picks %s and adds this note:"
	sendBackDrafts    = "Goes into its composer for you to edit and send:"
	sendBackSummary   = "Claude wrote it on request"
	sendBackWriting   = "Claude is writing a summary…"
	sendBackLast      = "The aside's last message"
	sendBackNothing   = "Nothing to send yet"
	sendBackCloseText = "close the aside after"
	sendBackAnswer    = "Answer question %d"
	sendBackDraftTo   = "Draft in its composer"
	sendBackCancel    = "Cancel"
	sendBackTextLines = 6
	sendBackHints     = "⌘↩ send back · ⎋ cancel"
	sendBackNoSummary = "No summary came back, so this is the aside's last message"
)

var sendBackKeys = []string{"cmd+return", "esc"}

type sendBackDraft struct {
	to           string
	from         string
	text         string
	close        bool
	filled       string
	stale        string
	staleLeft    int
	pending      bool
	asked        bool
	summaryAfter int
	summary      string
	note         string
}

func (d *sendBackDraft) edited() bool { return d.text != d.filled }

func (d *sendBackDraft) put(text string) {
	d.text, d.filled = text, text
}

func (d *sendBackDraft) refill(text string, pick bool) {
	if pick {
		d.stale, d.staleLeft = d.text, 1
	}
	d.put(text)
}

func (d *sendBackDraft) lastLimit() int {
	if d.asked {
		return d.summaryAfter
	}
	return 0
}

type sendBackView struct {
	Draft    sendBackDraft
	Parent   string
	Option   string
	Question int
	Writing  bool
	Note     string
}

func sendBackID(sid string) string { return sid + "/" + sendBackNode }

func sendBackFormID(sid string) string { return sendBackID(sid) + "/form" }

func (a *app) answerQuestion(rec record) (int, bool) {
	parent := a.session(rec.ParentSession)
	if parent == nil {
		return 0, false
	}
	an := rec.Aside.Anchor
	switch {
	case an.Elicitation != "":
		if _, _, ok := parent.pendingChoice(an.Elicitation, an.Field); !ok {
			return 0, false
		}
		el, found := parent.elicitation(an.Elicitation)
		if !found {
			return 0, false
		}
		n := 0
		for i, f := range el.Fields {
			if isChoice(f) {
				n++
			}
			if i == an.Field {
				return n, true
			}
		}
	case an.Open != "":
		n := 0
		for _, it := range parent.openList() {
			if it.Kind == itemOffer {
				continue
			}
			n++
			if it.ID == an.Open {
				return n, true
			}
		}
	}
	return 0, false
}

func lastAgentMessage(v transcriptView, after int) string {
	for i := len(v.Entries) - 1; i >= 0; i-- {
		en := v.Entries[i]
		if en.Turn <= after {
			return ""
		}
		if en.Kind == entryAgent && strings.TrimSpace(en.Text) != "" {
			return en.Text
		}
	}
	return ""
}

func lastAgentMessageUpTo(v transcriptView, upTo int) string {
	for i := len(v.Entries) - 1; i >= 0; i-- {
		en := v.Entries[i]
		if upTo > 0 && en.Turn > upTo {
			continue
		}
		if en.Kind == entryAgent && strings.TrimSpace(en.Text) != "" {
			return en.Text
		}
	}
	return ""
}

func (a *app) openSendBack(workspaceID string, s *session, slot string) error {
	rec := s.snapshot()
	if !rec.isAside() {
		return errNotAside
	}
	st := a.sessionState(workspaceID, rec.ID)
	to := sendDraft
	if _, ok := a.answerQuestion(rec); ok {
		to = sendAnswer
	}
	a.uiMu.Lock()
	open := st.sendBack != nil
	if !open {
		st.sendBack = &sendBackDraft{to: to, from: fromSummary, close: true}
	}
	a.uiMu.Unlock()
	if !open {
		if err := a.fillSendBack(workspaceID, s, st, false); err != nil {
			a.dropSendBack(st)
			return err
		}
	}
	a.focusAfterNode(workspaceID, slot, sendBackID(rec.ID))
	return nil
}

func (a *app) fillSendBack(workspaceID string, s *session, st *sessionUI, pick bool) error {
	a.uiMu.Lock()
	d := st.sendBack
	if d == nil || d.edited() {
		a.uiMu.Unlock()
		return nil
	}
	from, summary, pending, limit := d.from, d.summary, d.pending, d.lastLimit()
	a.uiMu.Unlock()
	switch {
	case from == fromSummary && summary != "":
		a.uiMu.Lock()
		d.refill(summary, pick)
		d.note = ""
		a.uiMu.Unlock()
		return nil
	case from == fromSummary && pending:
		a.uiMu.Lock()
		d.refill("", pick)
		a.uiMu.Unlock()
		return nil
	case from == fromLast || turnActive(s.snapshot()):
		text := lastAgentMessageUpTo(s.view(0), limit)
		a.uiMu.Lock()
		d.from, d.note = fromLast, ""
		d.refill(text, pick)
		a.uiMu.Unlock()
		return nil
	}
	turn := s.currentTurn()
	if err := a.prompt(workspaceID, promptArgs{Session: s.snapshot().ID, Text: asideSummaryText}); err != nil {
		return err
	}
	a.uiMu.Lock()
	d.refill("", pick)
	d.pending, d.asked, d.summaryAfter, d.note = true, true, turn, ""
	a.uiMu.Unlock()
	return nil
}

func (a *app) sendBackScreen(workspaceID string, s *session, v transcriptView) *sendBackView {
	rec := v.Session
	if !rec.isAside() {
		return nil
	}
	st := a.sessionState(workspaceID, rec.ID)
	n, answerable := a.answerQuestion(rec)
	parent := ""
	if p := a.session(rec.ParentSession); p != nil {
		parent = p.snapshot().Title
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	d := st.sendBack
	if d == nil {
		return nil
	}
	if d.to == sendAnswer && !answerable {
		d.to = sendDraft
	}
	writing := false
	if d.pending {
		text := lastAgentMessage(v, d.summaryAfter)
		ended := rec.Status == statusWaiting || !turnActive(rec)
		switch {
		case text != "" && !turnActive(rec):
			d.summary, d.pending = text, false
			if d.from == fromSummary && !d.edited() {
				d.put(text)
			}
		case ended:
			d.pending = false
			if d.from == fromSummary {
				d.from, d.note = fromLast, sendBackNoSummary
				if !d.edited() {
					d.put(lastAgentMessageUpTo(v, d.summaryAfter))
				}
			}
		default:
			writing = d.from == fromSummary
		}
	}
	view := sendBackView{Draft: *d, Parent: parent, Option: rec.Aside.Anchor.Option, Writing: writing, Note: d.note}
	if answerable {
		view.Question = n
	}
	return &view
}

func sendBackPanel(sid string, v sendBackView) sdui.Node {
	form := sendBackFormID(sid)
	d := v.Draft
	toOptions := []sdui.ChoiceOption{}
	if v.Question > 0 {
		toOptions = append(toOptions, sdui.ChoiceOption{Value: sendAnswer, Label: fmt.Sprintf(sendBackAnswer, v.Question), On: d.to == sendAnswer})
	}
	toOptions = append(toOptions, sdui.ChoiceOption{Value: sendDraft, Label: sendBackDraftTo, On: d.to == sendDraft})
	fromOptions := []sdui.ChoiceOption{
		{Value: fromSummary, Label: "Summary", Description: sendBackSummary, On: d.from == fromSummary},
		{Value: fromLast, Label: "Last message", Description: sendBackLast, On: d.from == fromLast},
	}
	line := sendBackDrafts
	if d.to == sendAnswer && v.Option != "" {
		line = fmt.Sprintf(sendBackPicks, v.Option)
	}
	placeholder := sendBackNothing
	if v.Writing {
		placeholder = sendBackWriting
	}
	text := sdui.WithLines(sdui.TextField(form+"/text", d.text, placeholder), sendBackTextLines)
	send := sdui.WithHint(sdui.WithVariant(sdui.WithForm(sdui.WithAction(sdui.Button(form+"/send", sendBackLabel, ""), actSendBackSend)), sdui.VariantPrimary), "cmd+return")
	if v.Writing || strings.TrimSpace(d.text) == "" {
		send = sdui.WithDisabled(send)
	}
	cancel := sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(form+"/cancel", sendBackCancel, ""), actSendBackCancel), sdui.VariantGhost), "esc")
	rows := []sdui.Node{
		sdui.Text(fmt.Sprintf(sendBackTitle, v.Parent)),
		sdui.Choice(form+"/to", sdui.ChoiceProps{Style: "radio", Options: toOptions}),
		sdui.MutedText(line),
		sdui.WithAction(sdui.Choice(form+"/from", sdui.ChoiceProps{Style: "radio", Options: fromOptions}), actSendBackFrom),
		text,
		sdui.WithGap(sdui.HStack(send, cancel, sdui.Spacer(), sdui.Checkbox(form+"/close", sendBackCloseText, d.close)), openGap),
	}
	if v.Note != "" {
		rows = slices.Insert(rows, len(rows)-1, sdui.WithTone(sdui.MutedText(v.Note), "warning"))
	}
	card := sdui.WithAction(sdui.WithAutosave(sdui.Form(form, sdui.WithRows(sdui.Card("", sendBackIcon, rows...)))), actSendBackDraft)
	return sdui.WithHints(sdui.WithAction(sdui.Keys(sendBackID(sid), sendBackKeys, card), actSendBackKey), sendBackHints)
}

func (d *sendBackDraft) fill(sid string, values map[string]any) (fromChanged bool) {
	form := sendBackFormID(sid)
	if to, ok := values[form+"/to"].(string); ok && to != "" {
		d.to = to
	}
	if from, ok := values[form+"/from"].(string); ok && from != "" && from != d.from {
		d.from, fromChanged = from, true
	}
	stale := d.staleLeft > 0
	if stale {
		d.staleLeft--
	}
	if text, ok := values[form+"/text"].(string); ok && text != d.text && (!stale || text != d.stale) {
		d.text = text
	}
	if c, ok := values[form+"/close"].(bool); ok {
		d.close = c
	}
	return fromChanged
}

func (a *app) sendBackAct(ctx context.Context, ev pluginsdk.UIEvent, s *session) error {
	rec := s.snapshot()
	st := a.sessionState(ev.WorkspaceID, rec.ID)
	switch ev.Action {
	case actSendBackOpen:
		return a.openSendBack(ev.WorkspaceID, s, ev.Slot)
	case actSendBackCancel:
		a.dropSendBack(st)
		return nil
	case actSendBackKey:
		var combo string
		if err := decode(ev.Value, &combo); err != nil {
			return err
		}
		if combo == "esc" {
			a.dropSendBack(st)
			return nil
		}
		return a.sendBack(ctx, ev, s, st)
	}
	var values map[string]any
	if err := decode(ev.Value, &values); err != nil {
		return err
	}
	a.uiMu.Lock()
	d := st.sendBack
	changed := false
	if d != nil {
		changed = d.fill(rec.ID, values)
	}
	a.uiMu.Unlock()
	if d == nil {
		return nil
	}
	if changed {
		if err := a.fillSendBack(ev.WorkspaceID, s, st, true); err != nil {
			return err
		}
	}
	if ev.Action == actSendBackSend {
		return a.sendBack(ctx, ev, s, st)
	}
	return nil
}

func (a *app) dropSendBack(st *sessionUI) {
	a.uiMu.Lock()
	st.sendBack = nil
	a.uiMu.Unlock()
}

func (a *app) sendBack(ctx context.Context, ev pluginsdk.UIEvent, s *session, st *sessionUI) error {
	a.uiMu.Lock()
	d := st.sendBack
	var draft sendBackDraft
	if d != nil {
		draft = *d
	}
	a.uiMu.Unlock()
	if d == nil || strings.TrimSpace(draft.text) == "" {
		return nil
	}
	rec := s.snapshot()
	parent := rec.ParentSession
	cmd := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID}
	closeAfter := draft.close
	if _, err := a.asideSendCommand(ctx, cmd, asideSendArgs{Session: rec.ID, To: draft.to, Text: draft.text, Close: &closeAfter}); err != nil {
		return err
	}
	a.dropSendBack(st)
	if !closeAfter {
		return nil
	}
	a.setBeside(ev.WorkspaceID, parent, "")
	if err := a.selectSession(ctx, ev.WorkspaceID, parent); err != nil {
		return err
	}
	if draft.to == sendAnswer {
		a.anchorAside(ev.WorkspaceID, rec)
	}
	a.focusAfterNode(ev.WorkspaceID, mainSlot, parent+"/composer")
	return nil
}
