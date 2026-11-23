package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"clubmate/pkg/sdui"
)

const (
	firstRunKey = "firstRun"
	hintSpent   = "-"

	firstPrompt  = "Find one small bug, fix it and run the tests"
	needsYouNote = "The agent waits until you answer. Anything waiting also shows under Needs you and in notifications."
	phoneOffer   = "Answer Claude from your iPhone when you step away."
	phonePair    = "Pair iPhone…"
	phoneNotNow  = "Not now"

	remoteStatusCmd     = "remote.status"
	remoteOfferCmd      = "remote.offer"
	remoteDismissCmd    = "remote.dismiss-offer"
	remoteChangedTopic  = "remote.changed"
	companionOfferState = "offer"
	actPhoneNotNow      = "phoneNotNow"
)

type firstRunHints struct {
	EmptyFirst bool   `json:"emptyFirst,omitempty"`
	Session    string `json:"session,omitempty"`
	NeedsYou   string `json:"needsYou,omitempty"`
	Phone      string `json:"phone,omitempty"`
}

type hintStore interface {
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
}

type firstRun struct {
	mu    sync.Mutex
	kv    hintStore
	hints firstRunHints
}

type companionWatch struct {
	state atomic.Pointer[string]
}

func spentHints() firstRunHints {
	return firstRunHints{EmptyFirst: true, Session: hintSpent, NeedsYou: hintSpent, Phone: hintSpent}
}

func (f *firstRun) load(ctx context.Context, kv hintStore, existing bool) error {
	var h firstRunHints
	found, err := kv.StorageGet(ctx, firstRunKey, &h)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kv = kv
	if found {
		f.hints = h
		return nil
	}
	if !existing {
		return nil
	}
	f.hints = spentHints()
	return kv.StorageSet(ctx, firstRunKey, f.hints)
}

func (f *firstRun) get() firstRunHints {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hints
}

func (f *firstRun) claim(change func(*firstRunHints) bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.hints
	if !change(&next) {
		return false
	}
	if f.kv != nil {
		if err := f.kv.StorageSet(context.Background(), firstRunKey, next); err != nil {
			fmt.Fprintln(os.Stderr, "agents: store first-run hints:", err)
			return false
		}
	}
	f.hints = next
	return true
}

func hintKey(sid, id string) string {
	return sid + "/" + id
}

func firstSessionKind(rec record) bool {
	return rec.Aside == nil && !rec.restricted() && !rec.lean()
}

func (a *app) firstSessionCreated(rec record) {
	if !firstSessionKind(rec) {
		return
	}
	a.firstRun.claim(func(h *firstRunHints) bool {
		if h.Session != "" {
			return false
		}
		h.Session, h.EmptyFirst = rec.ID, true
		return true
	})
}

func (a *app) firstPermission(sid, request string) {
	a.firstRun.claim(func(h *firstRunHints) bool {
		if h.NeedsYou != "" {
			return false
		}
		h.NeedsYou = hintKey(sid, request)
		return true
	})
}

func (a *app) firstTurnDone(s *session, turn int) {
	rec := s.snapshot()
	if !a.firstRun.phoneDue(rec.ID) {
		return
	}
	go func() {
		state, ok := a.firstRun.offerPhone(rec.ID, turn, func() (string, error) {
			var st remoteStatus
			err := a.host.RunCommand(context.Background(), remoteOfferCmd, "", nil, &st)
			return st.Companion.State, err
		})
		if ok {
			a.setCompanion(state)
		}
	}()
}

func (f *firstRun) phoneDue(sid string) bool {
	h := f.get()
	return h.Phone == "" && h.Session == sid
}

func (f *firstRun) offerPhone(sid string, turn int, offer func() (string, error)) (string, bool) {
	if !f.phoneDue(sid) {
		return "", false
	}
	state, err := offer()
	if err != nil {
		logError("offer a phone", err)
		return "", false
	}
	claimed := f.claim(func(h *firstRunHints) bool {
		if h.Phone != "" || h.Session != sid {
			return false
		}
		h.Phone = hintKey(sid, strconv.Itoa(turn))
		return true
	})
	return state, claimed
}

func (a *app) suggestionFor(rec record) string {
	if rec.Turns > 0 || rec.closed() || a.firstRun.get().Session != rec.ID {
		return ""
	}
	return firstPrompt
}

func permissionNote(needsYou, sid string, en *entry) string {
	if en.Kind != entryPermission || en.Outcome != nil || needsYou != hintKey(sid, en.ID) {
		return ""
	}
	return needsYouNote
}

func (a *app) emptyFirst() bool {
	return !a.firstRun.get().EmptyFirst
}

func (a *app) offerTurn(sid string) int {
	if a.companion() != companionOfferState {
		return 0
	}
	rest, ok := strings.CutPrefix(a.firstRun.get().Phone, sid+"/")
	if !ok {
		return 0
	}
	turn, _ := strconv.Atoi(rest)
	return turn
}

func offerRow(sid string) sdui.Node {
	pair := sdui.WithClientAction(sdui.WithVariant(sdui.Button(sid+"/phone/pair", phonePair, ""), sdui.VariantSecondary), sdui.ClientPairDevice)
	notNow := sdui.WithAction(sdui.WithVariant(sdui.Button(sid+"/phone/not-now", phoneNotNow, ""), sdui.VariantGhost), actPhoneNotNow)
	return sdui.Offer(sid+"/phone/offer", phoneOffer, "iphone", pair, notNow)
}

type remoteStatus struct {
	Companion struct {
		State string `json:"state"`
	} `json:"companion"`
}

func (a *app) companion() string {
	if p := a.companionState.state.Load(); p != nil {
		return *p
	}
	return ""
}

func (a *app) setCompanion(state string) {
	if old := a.companionState.state.Swap(&state); old != nil && *old == state {
		return
	}
	if a.firstRun.get().Phone == "" || a.firstRun.get().Phone == hintSpent {
		return
	}
	a.renderAll(context.Background())
}

func (a *app) loadCompanion(ctx context.Context) {
	var st remoteStatus
	if err := a.host.RunCommand(ctx, remoteStatusCmd, "", nil, &st); err != nil {
		return
	}
	a.setCompanion(st.Companion.State)
}

func (a *app) phoneNotNow(ctx context.Context, caller string) error {
	var st remoteStatus
	if err := a.host.RunCommandFor(ctx, caller, remoteDismissCmd, "", nil, &st); err != nil {
		return err
	}
	a.setCompanion(st.Companion.State)
	return nil
}
