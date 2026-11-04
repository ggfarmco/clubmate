package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	reviewIDBytes = 4
	layoutInline  = "inline"
	layoutSplit   = "split"
	categoryCode  = "code"
	categoryTest  = "test"
	categoryGen   = "generated"
	reviewDB      = "review.db"
)

var (
	errUnknownReview = errors.New("unknown review")
	errBadLayout     = errors.New("layout must be inline or split")
)

type reviewKey struct{ Worktree, Base, Head string }

type review struct {
	ID             string
	Workspace      string
	Key            reviewKey
	Root           string
	Tab            int
	Opener         string
	OpenerTitle    string
	Recipient      string
	RecipientTitle string
	Sent           int
	Info           reviewInfo
	Entries        []reviewEntry
	Comments       []reviewComment
	Filters        reviewFilters
	DefaultTitle   string
	Sheet          *sendSheet
	metaSent       *reviewMeta
	seq            int
	nv             reviewer
}

type reviewHost interface {
	Render(ctx context.Context, workspaceID, slot string, n sdui.Node) (int64, error)
	Publish(ctx context.Context, topic string, payload any) (pluginsdk.Event, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	RunCommand(ctx context.Context, id, workspaceID string, args any, out any) error
}

type reviewPayload struct {
	WorkspaceID string         `json:"workspaceId"`
	ID          string         `json:"id"`
	Worktree    string         `json:"worktree"`
	Base        string         `json:"base"`
	Head        string         `json:"head"`
	Opener      string         `json:"opener"`
	Recipient   string         `json:"recipient"`
	Reviewed    int            `json:"reviewed"`
	Total       int            `json:"total"`
	Open        int            `json:"open"`
	Sent        int            `json:"sent"`
	Closed      bool           `json:"closed"`
	Comments    []commentState `json:"comments,omitempty"`
}

type commentState struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

func reviewID(k reviewKey) string {
	sum := sha256.Sum256([]byte(k.Worktree + "\x00" + k.Base + "\x00" + k.Head))
	return fmt.Sprintf("rv_%x", sum[:reviewIDBytes])
}

func newReview(ws, root string, key reviewKey, tab int, nv reviewer) *review {
	return &review{
		ID:        reviewID(key),
		Workspace: ws,
		Key:       key,
		Root:      root,
		Tab:       tab,
		Filters:   reviewFilters{Code: reviewFilter{Shown: true}, Reviewed: reviewFilter{Shown: true}},
		nv:        nv,
	}
}

func (r *review) categoryShown(e reviewEntry) bool {
	switch e.Category {
	case categoryTest:
		return r.Filters.Tests.Shown
	case categoryGen:
		return r.Filters.Generated.Shown
	}
	return r.Filters.Code.Shown
}

func (r *review) shownEntries() []reviewEntry {
	var out []reviewEntry
	for _, e := range r.Entries {
		if r.categoryShown(e) {
			out = append(out, e)
		}
	}
	return out
}

func reviewedNow(e reviewEntry) bool {
	return e.Reviewed && !e.Stale
}

func (r *review) progress() (int, int) {
	shown := r.shownEntries()
	done := 0
	for _, e := range shown {
		if reviewedNow(e) {
			done++
		}
	}
	return done, len(shown)
}

func (r *review) openComments() []reviewComment {
	var out []reviewComment
	for _, c := range r.Comments {
		if c.Status == "open" {
			out = append(out, c)
		}
	}
	return out
}

func (r *review) payload(closed bool) reviewPayload {
	done, total := r.progress()
	var states []commentState
	for _, c := range r.Comments {
		states = append(states, commentState{ID: c.ID, Status: c.Status})
	}
	return reviewPayload{
		WorkspaceID: r.Workspace, ID: r.ID, Worktree: r.Key.Worktree, Base: r.Key.Base, Head: r.Key.Head,
		Opener: r.Opener, Recipient: r.Recipient, Reviewed: done, Total: total, Open: len(r.openComments()), Sent: r.Sent, Closed: closed,
		Comments: states,
	}
}

func (r *review) refresh(comments bool) error {
	if filters, err := r.nv.reviewFilters(r.Tab); err == nil {
		r.Filters = filters
	} else {
		logError("review filters", err)
	}
	entries, err := r.nv.reviewEntries(r.Tab)
	if err != nil {
		return err
	}
	r.Entries = entries
	if comments {
		list, err := r.nv.reviewComments(r.Tab)
		if err != nil {
			return err
		}
		r.Comments = list
	}
	return nil
}

func (a *app) reviewEvent(h reviewHost, e *editor, ev nvimEvent) {
	go a.applyReviewEvent(context.Background(), h, e.id, e.root, ev)
}

func (a *app) applyReviewEvent(ctx context.Context, h reviewHost, ws, root string, ev nvimEvent) {
	if ev.Kind == "send" {
		if err := a.sendAt(ctx, h, ws, root, ev.Tab); err != nil {
			logError("review send", err)
		}
		return
	}
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	r := a.reviewAtLocked(ws, root, ev.Tab)
	if r == nil {
		return
	}
	if ev.Kind == "closed" {
		a.removeReviewLocked(ctx, h, r)
		a.renderReviewsLocked(ctx, h, ws)
		return
	}
	if err := r.refresh(ev.Kind != "file_open"); err != nil {
		logError("review "+ev.Kind, err)
	}
	a.reviewChangedLocked(ctx, h, r)
}

func (a *app) reviewAtLocked(ws, root string, tab int) *review {
	for _, r := range a.reviews {
		if r.Workspace == ws && r.Root == root && r.Tab == tab {
			return r
		}
	}
	return nil
}

func (a *app) workspaceReviewsLocked(ws string) []*review {
	var out []*review
	for _, r := range a.reviews {
		if r.Workspace == ws {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(x, y *review) int { return cmp.Compare(x.seq, y.seq) })
	return out
}

func (a *app) addReviewLocked(r *review) {
	a.reviewSeq++
	r.seq = a.reviewSeq
	a.reviews[r.ID] = r
	a.reviewSel[r.Workspace] = r.ID
}

func (a *app) removeReviewLocked(ctx context.Context, h reviewHost, r *review) {
	delete(a.reviews, r.ID)
	if a.reviewSel[r.Workspace] == r.ID {
		delete(a.reviewSel, r.Workspace)
		if rest := a.workspaceReviewsLocked(r.Workspace); len(rest) > 0 {
			a.reviewSel[r.Workspace] = rest[len(rest)-1].ID
		}
	}
	a.publishReviewLocked(ctx, h, r, true)
	delete(a.reviewPublished, r.ID)
}

func (a *app) reviewChangedLocked(ctx context.Context, h reviewHost, r *review) {
	a.publishReviewLocked(ctx, h, r, false)
	a.renderReviewsLocked(ctx, h, r.Workspace)
}

func (a *app) publishReviewLocked(ctx context.Context, h reviewHost, r *review, closed bool) {
	p := r.payload(closed)
	if last, ok := a.reviewPublished[r.ID]; ok && reflect.DeepEqual(last, p) {
		return
	}
	a.reviewPublished[r.ID] = p
	if _, err := h.Publish(ctx, reviewTopic, p); err != nil {
		logError("publish review", err)
	}
}

func (a *app) renderReviews(ctx context.Context, h reviewHost, ws string) {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	a.renderReviewsLocked(ctx, h, ws)
}

func (a *app) renderReviewsLocked(ctx context.Context, h reviewHost, ws string) {
	rs := a.workspaceReviewsLocked(ws)
	if _, err := h.Render(ctx, ws, reviewSlot, reviewView(rs)); err != nil {
		logError("render review", err)
	}
	if hint := a.reviewStatusHint; hint != nil {
		hint(ws)
	}
}

func isReviewDB(path string) bool {
	return strings.HasPrefix(filepath.Base(path), reviewDB)
}

func (a *app) reviewDBChanged(ctx context.Context, h reviewHost, ws string) {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	poked := map[reviewer]bool{}
	for _, r := range a.workspaceReviewsLocked(ws) {
		if err := r.refresh(true); err != nil {
			logError("review comments", err)
		}
		a.reviewChangedLocked(ctx, h, r)
		if !poked[r.nv] {
			poked[r.nv] = true
			if err := r.nv.reviewCommentsChanged(); err != nil {
				logError("review comments changed", err)
			}
		}
	}
}

func (a *app) reviewStatus(ws, root string, tab int) (bool, int) {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	r := a.reviewAtLocked(ws, root, tab)
	if r == nil {
		return false, 0
	}
	return true, len(r.openComments())
}

func (a *app) dropReviewsOf(ctx context.Context, h reviewHost, nv reviewer) {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	touched := map[string]bool{}
	for _, r := range a.reviews {
		if r.nv == nv {
			a.removeReviewLocked(ctx, h, r)
			touched[r.Workspace] = true
		}
	}
	for ws := range touched {
		a.renderReviewsLocked(ctx, h, ws)
	}
}

func (a *app) reviewUIEvent(ctx context.Context, h reviewHost, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case "sendReview":
		return a.submitSend(ctx, h, ev.WorkspaceID, ev.Value)
	case "cancel":
		return a.closeSendSheet(ctx, h, ev.WorkspaceID)
	case "layout":
		a.reviewMu.Lock()
		defer a.reviewMu.Unlock()
		return a.setReviewLayoutLocked(ev.WorkspaceID, decodeString(ev.Value))
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) selectReviewLocked(ctx context.Context, r *review) error {
	a.reviewSel[r.Workspace] = r.ID
	if a.switchRoot != nil {
		a.switchRoot(ctx, r)
	}
	return r.nv.reviewFocus(r.Tab)
}

func (a *app) showReview(ctx context.Context, h *pluginsdk.Host, ws, id string) error {
	a.reviewMu.Lock()
	r := a.reviews[id]
	if r == nil || r.Workspace != ws {
		a.reviewMu.Unlock()
		return fmt.Errorf("%w: %s", errUnknownReview, id)
	}
	err := a.selectReviewLocked(ctx, r)
	if err == nil {
		a.reviewChangedLocked(ctx, h, r)
	}
	a.reviewMu.Unlock()
	if err != nil {
		return err
	}
	focusMain(ctx, h, ws)
	return nil
}

func (a *app) setReviewLayoutLocked(ws, layout string) error {
	if layout != layoutInline && layout != layoutSplit {
		return errBadLayout
	}
	a.reviewLayouts[ws] = layout
	var errs []error
	for _, r := range a.workspaceReviewsLocked(ws) {
		errs = append(errs, r.nv.reviewLayout(r.Tab, layout))
	}
	return errors.Join(errs...)
}

func decodeString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
