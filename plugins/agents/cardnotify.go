package main

import (
	"cmp"
	"fmt"
	"slices"

	"clubmate/plugins/agents/api"
)

const cardMessageFormat = "%s · %s"

func (r record) openCards() []string {
	if r.closed() {
		return nil
	}
	return r.gateRequests()
}

func (a *app) cardsSynced(s *session) {
	s.mu.Lock()
	rec, was := s.rec, s.cards
	now := rec.openCards()
	s.cards = now
	s.mu.Unlock()
	for _, request := range now {
		if !slices.Contains(was, request) {
			a.cardOpened(rec, request)
		}
	}
	for _, request := range was {
		if !slices.Contains(now, request) {
			a.notes.resolved(s, api.RequestGate, request)
		}
	}
}

func (a *app) cardOpened(rec record, request string) {
	a.notes.superseded(rec.ID)
	if a.effectiveNotify(rec) == notifyMuted {
		return
	}
	a.notes.carded(notifyPost{WorkspaceID: rec.Workspace, Session: rec.ID, Collapse: rec.ID, Title: rec.Title, Message: cardMessage(rec, request), Kind: notifyAsks, Request: request})
}

func cardMessage(rec record, request string) string {
	if rec.heldWaits() && rec.Held.Request == request {
		return fmt.Sprintf(cardMessageFormat, heldCardTitle(*rec.Held), heldQuestion(*rec.Held))
	}
	if holds := rec.cardHolds(request); len(holds) > 0 {
		return fmt.Sprintf(cardMessageFormat, callCardTitle(holds), callAsk)
	}
	i := slices.IndexFunc(rec.LandReviews, func(st landReview) bool { return st.Request == request })
	if i < 0 {
		return rec.queueAskMessage(request)
	}
	st := rec.LandReviews[i]
	return fmt.Sprintf(cardMessageFormat, gateCardTitle(st), fmt.Sprintf(gateCardQuestion, cmp.Or(st.Source, st.Into)))
}
