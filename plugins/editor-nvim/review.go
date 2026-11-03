package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	reviewIDBytes  = 4
	layoutInline   = "inline"
	layoutSplit    = "split"
	categoryCode   = "code"
	categoryTest   = "test"
	categoryGen    = "generated"
	commentsRootID = "comments"
)

var (
	errUnknownReview = errors.New("unknown review")
	errBadLayout     = errors.New("layout must be inline or split")
)

type reviewKey struct{ Worktree, Base, Head string }

type reviewFilters struct{ Code, Tests, Generated, Reviewed bool }

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
	Counts         reviewCategories
	Current        string
	Collapsed      map[string]bool
	CommentsFolded bool
	seq            int
	nv             reviewer
}

type reviewHost interface {
	Render(ctx context.Context, workspaceID, slot string, n sdui.Node) (int64, error)
	Publish(ctx context.Context, topic string, payload any) (pluginsdk.Event, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
}

type reviewPayload struct {
	WorkspaceID string `json:"workspaceId"`
	ID          string `json:"id"`
	Worktree    string `json:"worktree"`
	Base        string `json:"base"`
	Head        string `json:"head"`
	Opener      string `json:"opener"`
	Recipient   string `json:"recipient"`
	Reviewed    int    `json:"reviewed"`
	Total       int    `json:"total"`
	Open        int    `json:"open"`
	Sent        int    `json:"sent"`
	Closed      bool   `json:"closed"`
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
		Filters:   reviewFilters{Code: true, Reviewed: true},
		Collapsed: map[string]bool{},
		nv:        nv,
	}
}

func (r *review) categoryShown(e reviewEntry) bool {
	switch e.Category {
	case categoryTest:
		return r.Filters.Tests
	case categoryGen:
		return r.Filters.Generated
	}
	return r.Filters.Code
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
	return reviewPayload{
		WorkspaceID: r.Workspace, ID: r.ID, Worktree: r.Key.Worktree, Base: r.Key.Base, Head: r.Key.Head,
		Opener: r.Opener, Recipient: r.Recipient, Reviewed: done, Total: total, Open: len(r.openComments()), Sent: r.Sent, Closed: closed,
	}
}

func (r *review) entry(path string) (reviewEntry, bool) {
	i := slices.IndexFunc(r.Entries, func(e reviewEntry) bool { return e.Path == path })
	if i < 0 {
		return reviewEntry{}, false
	}
	return r.Entries[i], true
}

func (r *review) refresh(comments bool) error {
	if counts, err := r.nv.reviewFilter(r.Tab, "", false); err == nil {
		r.Counts = counts
		r.Filters.Tests, r.Filters.Generated = !counts.HideTests, !counts.HideGenerated
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
	if ev.Kind == "file_open" {
		r.Current = ev.Path
		a.reviewSel[ws] = r.ID
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
	if last, ok := a.reviewPublished[r.ID]; ok && last == p {
		return
	}
	a.reviewPublished[r.ID] = p
	if _, err := h.Publish(ctx, reviewTopic, p); err != nil {
		logError("publish review", err)
	}
}

func (a *app) renderReviewsLocked(ctx context.Context, h reviewHost, ws string) {
	rs := a.workspaceReviewsLocked(ws)
	if _, err := h.Render(ctx, ws, reviewSlot, reviewView(rs, a.reviewSel[ws])); err != nil {
		logError("render review", err)
	}
	if hint := a.reviewStatusHint; hint != nil {
		hint(ws)
	}
}

func (a *app) reviewOpen(ws string) (bool, string) {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	return len(a.workspaceReviewsLocked(ws)) > 0, a.reviewLayouts[ws]
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
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	if ev.Action == "layout" {
		return a.setReviewLayoutLocked(ctx, h, ev.WorkspaceID, decodeString(ev.Value))
	}
	r := a.reviews[a.reviewSel[ev.WorkspaceID]]
	if ev.Action == "switch" {
		r = a.reviews[decodeString(ev.Value)]
	}
	if r == nil || r.Workspace != ev.WorkspaceID {
		return errUnknownReview
	}
	value := decodeString(ev.Value)
	switch {
	case ev.Action == "switch":
		a.reviewSel[ev.WorkspaceID] = r.ID
		if a.switchRoot != nil {
			a.switchRoot(ctx, r)
		}
		if err := r.nv.reviewFocus(r.Tab); err != nil {
			return err
		}
	case ev.Action == "select" && strings.HasPrefix(value, "c:"):
		id := strings.TrimPrefix(value, "c:")
		i := slices.IndexFunc(r.Comments, func(c reviewComment) bool { return fmt.Sprint(c.ID) == id })
		if i < 0 {
			return nil
		}
		c := r.Comments[i]
		return r.nv.reviewJump(r.Tab, c.File, c.LineStart, c.Side)
	case ev.Action == "select":
		if path, ok := strings.CutPrefix(value, "file:"); ok {
			r.Current = path
			return r.nv.reviewSelect(r.Tab, path)
		}
		return nil
	case ev.Action == "expand" || ev.Action == "collapse":
		folded := ev.Action == "collapse"
		if value == commentsRootID {
			r.CommentsFolded = folded
		} else {
			r.Collapsed[strings.TrimPrefix(value, "dir:")] = folded
		}
	case ev.Action == "toggleReviewed":
		path := strings.TrimPrefix(value, "file:")
		e, ok := r.entry(path)
		if !ok {
			return nil
		}
		if err := r.nv.reviewSetReviewed(r.Tab, path, !reviewedNow(e)); err != nil {
			return err
		}
		if err := r.refresh(false); err != nil {
			return err
		}
	case ev.Action == "filter":
		name, on := strings.TrimPrefix(ev.NodeID, "review/filters/"), decodeBool(ev.Value)
		if name == "tests" || name == "generated" {
			if _, err := r.nv.reviewFilter(r.Tab, name, !on); err != nil {
				return err
			}
			if err := r.refresh(false); err != nil {
				return err
			}
			break
		}
		r.setFilter(name, on)
	case ev.Action == "showReviewed":
		r.Filters.Reviewed = true
	case ev.Action == "prev" || ev.Action == "next":
		if path := r.neighbour(ev.Action == "next"); path != "" {
			r.Current = path
			if err := r.nv.reviewSelect(r.Tab, path); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	a.reviewChangedLocked(ctx, h, r)
	return nil
}

func (r *review) setFilter(name string, on bool) {
	switch name {
	case "code":
		r.Filters.Code = on
	case "reviewed":
		r.Filters.Reviewed = on
	}
}

func (r *review) visibleFiles() []reviewEntry {
	var out []reviewEntry
	for _, e := range r.shownEntries() {
		if r.Filters.Reviewed || !reviewedNow(e) || e.Path == r.Current {
			out = append(out, e)
		}
	}
	return out
}

func (r *review) neighbour(next bool) string {
	files := r.visibleFiles()
	if len(files) == 0 {
		return ""
	}
	i := slices.IndexFunc(files, func(e reviewEntry) bool { return e.Path == r.Current })
	switch {
	case i < 0 && next:
		i = 0
	case i < 0:
		i = len(files) - 1
	case next:
		i = (i + 1) % len(files)
	default:
		i = (i - 1 + len(files)) % len(files)
	}
	return files[i].Path
}

func (a *app) setReviewLayoutLocked(ctx context.Context, h reviewHost, ws, layout string) error {
	if layout != layoutInline && layout != layoutSplit {
		return errBadLayout
	}
	a.reviewLayouts[ws] = layout
	var errs []error
	for _, r := range a.workspaceReviewsLocked(ws) {
		errs = append(errs, r.nv.reviewLayout(r.Tab, layout))
	}
	a.renderReviewsLocked(ctx, h, ws)
	return errors.Join(errs...)
}

func decodeString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func decodeBool(raw json.RawMessage) bool {
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}
