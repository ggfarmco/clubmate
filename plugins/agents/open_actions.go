package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
)

const (
	focusQueue    = "queue"
	focusComposer = "composer"
)

type openStop struct {
	session string
	item    openItem
}

func isOpenAction(action string) bool {
	switch action {
	case actOpenDraft, actOpenSend, actOpenLater, actOpenBacklog, actOpenNoAll, actOpenMove, actOpenShow, actOpenConvert, actOpenSession:
		return true
	}
	return false
}

func (a *app) openAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, st *sessionUI) error {
	sid := s.snapshot().ID
	cmd := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID}
	switch ev.Action {
	case actOpenDraft, actOpenSend:
		var values map[string]any
		if err := decode(ev.Value, &values); err != nil {
			return err
		}
		items := s.openList()
		a.uiMu.Lock()
		st.open.fill(sid, items, values)
		a.uiMu.Unlock()
		if ev.Action == actOpenSend {
			return a.sendOpen(ctx, ev.WorkspaceID, s, st)
		}
		return nil
	case actOpenLater:
		return a.laterOpen(ctx, cmd, s, st)
	case actOpenBacklog:
		_, err := a.openBacklogCommand(ctx, cmd, openPickArgs{Session: sid})
		return err
	case actOpenNoAll:
		_, err := a.openNoCommand(ctx, cmd, openPickArgs{Session: sid})
		return err
	case actOpenMove:
		var node string
		if err := decode(ev.Value, &node); err != nil {
			return err
		}
		item := strings.TrimPrefix(node, openFormNode(sid)+"/")
		if item == openNextNode {
			item = ""
			if offers := offerItems(s.openList()); len(offers) > 0 {
				item = offers[0].ID
			}
		}
		a.uiMu.Lock()
		st.open.cursor = item
		a.uiMu.Unlock()
		return a.queueSelect(ctx, ev.WorkspaceID, sid)
	case actOpenShow:
		en := s.openCard().Entry
		a.uiMu.Lock()
		st.expanded[en] = true
		a.uiMu.Unlock()
		return nil
	case actOpenConvert:
		_, err := a.openConvertCommand(ctx, cmd, sessionArgs{Session: sid})
		return err
	case actOpenSession:
		return a.showOpenSession(ctx, ev.WorkspaceID, sid)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) queueSelect(ctx context.Context, workspaceID, sid string) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	queue, selected := p.filter == filterOpen, p.selected
	a.uiMu.Unlock()
	if !queue || selected == sid {
		return nil
	}
	return a.selectSession(ctx, workspaceID, sid)
}

func (a *app) showOpenSession(ctx context.Context, workspaceID, sid string) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.filter, p.focusAfter = filterAll, focusComposer
	a.uiMu.Unlock()
	return a.selectSession(ctx, workspaceID, sid)
}

func (a *app) laterOpen(ctx context.Context, cmd pluginsdk.CommandRun, s *session, st *sessionUI) error {
	sid := s.snapshot().ID
	entry := s.openCard().Entry
	if _, err := a.openLaterCommand(ctx, cmd, sessionArgs{Session: sid}); err != nil {
		return err
	}
	a.uiMu.Lock()
	delete(st.expanded, entry)
	a.uiMu.Unlock()
	return nil
}

func (a *app) sendOpen(ctx context.Context, workspaceID string, s *session, st *sessionUI) error {
	sid := s.snapshot().ID
	items := s.openList()
	a.uiMu.Lock()
	args := st.open.args(sid, items)
	a.uiMu.Unlock()
	if args.picked() == 0 {
		return nil
	}
	_, err := a.answerOpen(ctx, s, workspaceID, args, nil)
	return err
}

func (a *app) sendAllOpen(ctx context.Context, workspaceID string) error {
	var errs []error
	for _, e := range a.openQueue(workspaceID) {
		s := a.session(e.Session)
		if s == nil {
			continue
		}
		errs = append(errs, a.sendOpen(ctx, workspaceID, s, a.sessionState(workspaceID, e.Session)))
	}
	return errors.Join(errs...)
}

func (a *app) openKey(ctx context.Context, ev pluginsdk.UIEvent) error {
	var combo string
	if err := decode(ev.Value, &combo); err != nil {
		return err
	}
	only := ""
	if ev.NodeID != openQueueNode {
		only, _, _ = strings.Cut(ev.NodeID, "/")
	}
	if combo == "cmd+return" && only == "" {
		return a.sendAllOpen(ctx, ev.WorkspaceID)
	}
	stops := a.openStopList(ev.WorkspaceID, only)
	if len(stops) == 0 {
		return nil
	}
	at := a.stopIndex(ev.WorkspaceID, only, stops)
	stop := stops[at]
	s, err := a.lookup(ev.WorkspaceID, stop.session)
	if err != nil {
		return err
	}
	st := a.sessionState(ev.WorkspaceID, stop.session)
	cmd := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID}
	switch combo {
	case "j", "down":
		return a.moveStop(ctx, ev.WorkspaceID, stops, min(at+1, len(stops)-1))
	case "k", "up":
		return a.moveStop(ctx, ev.WorkspaceID, stops, max(at-1, 0))
	case "1", "2", "3", "4":
		n, _ := strconv.Atoi(combo)
		a.pickNumber(st, stop, stopsOf(stops, stop.session), n-1)
	case "x":
		if stop.item.Kind == itemOffer {
			a.withDraft(st, func(d *openDraft) { d.tick(stop.item.ID) })
		}
	case "o":
		if stop.item.Kind != itemOffer {
			a.withDraft(st, func(d *openDraft) { d.openOther(stop.item) })
		}
	case "l":
		return a.laterOpen(ctx, cmd, s, st)
	case "b":
		_, err := a.openBacklogCommand(ctx, cmd, openPickArgs{Session: stop.session})
		return err
	case "delete":
		_, err := a.openNoCommand(ctx, cmd, openPickArgs{Session: stop.session, Items: []string{stop.item.ID}})
		return err
	case "shift+delete":
		_, err := a.openNoCommand(ctx, cmd, openPickArgs{Session: stop.session})
		return err
	case "cmd+return":
		return a.sendOpen(ctx, ev.WorkspaceID, s, st)
	case "return":
		return a.showOpenSession(ctx, ev.WorkspaceID, stop.session)
	}
	return nil
}

func (a *app) withDraft(st *sessionUI, edit func(*openDraft)) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	edit(&st.open)
}

func (a *app) pickNumber(st *sessionUI, stop openStop, items []openItem, i int) {
	if stop.item.Kind != itemOffer {
		a.withDraft(st, func(d *openDraft) { d.pick(stop.item, i) })
		return
	}
	offers := offerItems(items)
	if i < len(offers) {
		a.withDraft(st, func(d *openDraft) { d.tick(offers[i].ID) })
	}
}

func stopsOf(stops []openStop, sid string) []openItem {
	var items []openItem
	for _, stop := range stops {
		if stop.session == sid {
			items = append(items, stop.item)
		}
	}
	return items
}

func (a *app) openStopList(workspaceID, only string) []openStop {
	var stops []openStop
	add := func(sid string, items []openItem) {
		for _, it := range openStops(items) {
			stops = append(stops, openStop{session: sid, item: it})
		}
	}
	if only != "" {
		if s := a.session(only); s != nil {
			add(only, s.openList())
		}
		return stops
	}
	for _, e := range a.openQueue(workspaceID) {
		add(e.Session, e.Items)
	}
	return stops
}

func (a *app) stopIndex(workspaceID, only string, stops []openStop) int {
	sid := only
	if sid == "" {
		p := a.workspaceState(workspaceID)
		a.uiMu.Lock()
		sid = p.selected
		a.uiMu.Unlock()
	}
	st := a.sessionState(workspaceID, sid)
	a.uiMu.Lock()
	cursor := st.open.cursor
	a.uiMu.Unlock()
	if i := slices.IndexFunc(stops, func(stop openStop) bool { return stop.session == sid && stop.item.ID == cursor }); i >= 0 {
		return i
	}
	return max(slices.IndexFunc(stops, func(stop openStop) bool { return stop.session == sid }), 0)
}

func (a *app) moveStop(ctx context.Context, workspaceID string, stops []openStop, to int) error {
	stop := stops[to]
	st := a.sessionState(workspaceID, stop.session)
	a.withDraft(st, func(d *openDraft) { d.cursor = stop.item.ID })
	return a.queueSelect(ctx, workspaceID, stop.session)
}

func (a *app) openQueueScreen(workspaceID string, rows []sessionRow, selected string) openQueueScreen {
	byID := make(map[string]sessionRow, len(rows))
	for _, row := range rows {
		byID[row.Rec.ID] = row
	}
	q := openQueueScreen{}
	add := func(sid string) {
		s := a.session(sid)
		if s == nil {
			return
		}
		data := s.openCard()
		st := a.sessionState(workspaceID, sid)
		a.uiMu.Lock()
		st.open.prune(data.Items)
		draft := st.open.clone()
		a.uiMu.Unlock()
		q.Cards = append(q.Cards, openQueueCard{Rec: s.snapshot(), Branch: byID[sid].Branch, Data: data, Draft: draft})
	}
	for _, e := range a.openQueue(workspaceID) {
		add(e.Session)
	}
	if i := slices.IndexFunc(q.Cards, func(c openQueueCard) bool { return c.Rec.ID == selected }); i >= 0 {
		q.Cards[i].Cursor = q.Cards[i].Draft.current(q.Cards[i].Data.Items)
	} else if len(q.Cards) > 0 {
		q.Cards[0].Cursor = q.Cards[0].Draft.current(q.Cards[0].Data.Items)
	}
	for _, row := range rows {
		if row.Rec.MayAsk && row.Rec.OpenCount == 0 && !row.Rec.closed() {
			add(row.Rec.ID)
		}
	}
	return q
}

func (a *app) takeFocusAfter(workspaceID string) (string, string) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	focus := p.focusAfter
	p.focusAfter = ""
	return focus, p.selected
}
