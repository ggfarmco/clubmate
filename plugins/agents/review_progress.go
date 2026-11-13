package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"

	"clubmate/pkg/sdui"
)

const (
	reviewTopic    = "editor-nvim.review"
	editorReady    = "editor-nvim.ready"
	reviewShowCmd  = "editor-nvim.review-show"
	actOpenReview  = "openReview"
	openReviewNode = "openReview"
	reviewBoardKey = "reviews"
)

type reviewPayload struct {
	WorkspaceID string                 `json:"workspaceId"`
	ID          string                 `json:"id"`
	Worktree    string                 `json:"worktree"`
	Base        string                 `json:"base"`
	Head        string                 `json:"head"`
	Opener      string                 `json:"opener"`
	Recipient   string                 `json:"recipient"`
	Reviewed    int                    `json:"reviewed"`
	Total       int                    `json:"total"`
	Open        int                    `json:"open"`
	Sent        int                    `json:"sent"`
	Closed      bool                   `json:"closed"`
	Comments    []reviewPayloadComment `json:"comments"`
}

type reviewPayloadComment struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type savedBoard struct {
	Seq    int64                       `json:"seq"`
	Open   []reviewPayload             `json:"open,omitempty"`
	States map[string]map[int64]string `json:"states,omitempty"`
}

type reviewProgress struct {
	ID       string `json:"id"`
	Reviewed int    `json:"reviewed"`
	Total    int    `json:"total"`
	Open     int    `json:"open"`
	Sent     int    `json:"sent"`
	Text     string `json:"text"`
	Short    string `json:"short"`
}

type reviewBoard struct {
	mu       sync.Mutex
	open     map[string]reviewPayload
	seq      map[string]int
	states   map[string]map[int64]string
	closedIn map[string]string
	next     int
}

type reviewShowArgs struct {
	ID string `json:"id"`
}

func newReviewBoard() *reviewBoard {
	return &reviewBoard{open: map[string]reviewPayload{}, seq: map[string]int{}, states: map[string]map[int64]string{}, closedIn: map[string]string{}}
}

func (b *reviewBoard) apply(raw json.RawMessage) (string, []string, bool) {
	var p reviewPayload
	if json.Unmarshal(raw, &p) != nil || p.ID == "" {
		return "", nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	last := b.open[p.ID]
	people := slices.Compact(slices.Sorted(slices.Values([]string{p.Opener, p.Recipient, last.Opener, last.Recipient})))
	people = slices.DeleteFunc(people, func(id string) bool { return id == "" })
	if p.Comments != nil {
		states := map[int64]string{}
		for _, c := range p.Comments {
			states[c.ID] = c.Status
		}
		b.states[p.ID] = states
	}
	if p.Closed {
		b.closedIn[p.ID] = cmp.Or(p.Worktree, last.Worktree)
		delete(b.open, p.ID)
		delete(b.seq, p.ID)
		return p.WorkspaceID, people, true
	}
	b.next++
	b.open[p.ID], b.seq[p.ID] = p, b.next
	return p.WorkspaceID, people, true
}

func (b *reviewBoard) closeAll() map[string][]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	people := map[string][]string{}
	for _, p := range b.open {
		b.closedIn[p.ID] = p.Worktree
		for _, id := range []string{p.Opener, p.Recipient} {
			if id != "" && !slices.Contains(people[p.WorkspaceID], id) {
				people[p.WorkspaceID] = append(people[p.WorkspaceID], id)
			}
		}
	}
	for _, ids := range people {
		slices.Sort(ids)
	}
	clear(b.open)
	clear(b.seq)
	return people
}

func (b *reviewBoard) save(ctx context.Context, kv sessionStore, seq int64) error {
	b.mu.Lock()
	saved := savedBoard{Seq: seq, Open: slices.SortedFunc(maps.Values(b.open), func(x, y reviewPayload) int { return b.seq[x.ID] - b.seq[y.ID] }), States: b.states}
	raw, err := json.Marshal(saved)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	return kv.StorageSet(ctx, reviewBoardKey, json.RawMessage(raw))
}

func (b *reviewBoard) takeClosed() (map[string]map[int64]string, map[string]bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	closed, worktrees := map[string]map[int64]string{}, map[string]bool{}
	for id, states := range b.states {
		if _, ok := b.open[id]; ok {
			continue
		}
		closed[id] = states
		if wt := b.closedIn[id]; wt != "" {
			worktrees[wt] = true
		}
		delete(b.states, id)
	}
	clear(b.closedIn)
	return closed, worktrees
}

func (b *reviewBoard) load(ctx context.Context, kv sessionStore) (int64, error) {
	var saved savedBoard
	if found, err := kv.StorageGet(ctx, reviewBoardKey, &saved); err != nil || !found {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range saved.Open {
		b.next++
		b.open[p.ID], b.seq[p.ID] = p, b.next
	}
	maps.Copy(b.states, saved.States)
	return saved.Seq, nil
}

func (a *app) loadReviews(ctx context.Context, kv sessionStore) (int64, error) {
	seq, err := a.reviews.load(ctx, kv)
	if err != nil || !a.freezeClosedReviews(nil, true) {
		return seq, err
	}
	return seq, a.reviews.save(ctx, kv, seq)
}

func (a *app) freezeClosedReviews(people []*session, everyone bool) bool {
	closed, worktrees := a.reviews.takeClosed()
	if len(closed) == 0 {
		return false
	}
	for _, s := range a.allSessions() {
		if everyone || slices.Contains(people, s) || worktrees[s.snapshot().Worktree] {
			s.freezeReviews(closed)
		}
	}
	return true
}

func (a *app) reviewsChanged(ctx context.Context, seq int64, people map[string][]string) {
	var sessions []*session
	for _, ids := range people {
		for _, id := range ids {
			if s := a.session(id); s != nil {
				sessions = append(sessions, s)
			}
		}
	}
	a.freezeClosedReviews(sessions, false)
	if err := a.reviews.save(ctx, a.host, seq); err != nil {
		fmt.Fprintln(os.Stderr, "agents: save reviews:", err)
	}
	for _, s := range sessions {
		a.notes.mark(s, changeReview, false)
	}
	for ws := range people {
		a.kick(ws)
	}
}

func (b *reviewBoard) progress(session string) *reviewProgress {
	b.mu.Lock()
	defer b.mu.Unlock()
	var best reviewPayload
	newest := 0
	for id, p := range b.open {
		if (p.Opener == session || p.Recipient == session) && b.seq[id] > newest {
			best, newest = p, b.seq[id]
		}
	}
	if newest == 0 {
		return nil
	}
	return &reviewProgress{
		ID: best.ID, Reviewed: best.Reviewed, Total: best.Total, Open: best.Open, Sent: best.Sent,
		Text:  fmt.Sprintf("review %d/%d · %d", best.Reviewed, best.Total, best.Open),
		Short: shortProgress(best.Reviewed, best.Total, best.Open),
	}
}

func shortProgress(reviewed, total, open int) string {
	if open == 0 {
		return fmt.Sprintf("%d/%d", reviewed, total)
	}
	return fmt.Sprintf("%d/%d · %d", reviewed, total, open)
}

func (b *reviewBoard) cards(frozen map[string]map[int64]string) (map[string]map[int64]string, map[string]bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	states := make(map[string]map[int64]string, len(frozen)+len(b.states))
	maps.Copy(states, frozen)
	maps.Copy(states, b.states)
	open := make(map[string]bool, len(b.open))
	for id := range b.open {
		open[id] = true
	}
	return states, open
}

func reviewBadge(p *reviewProgress) sdui.HeaderBadge {
	tone := ""
	if p.Total > 0 && p.Reviewed == p.Total {
		tone = "success"
	}
	if p.Sent > 0 {
		return sdui.HeaderBadge{Text: fmt.Sprintf("Review %d/%d · %d %s sent", p.Reviewed, p.Total, p.Sent, plural(p.Sent, "comment", "comments")), Tone: tone}
	}
	return sdui.HeaderBadge{Text: fmt.Sprintf("Review %d/%d · %d %s", p.Reviewed, p.Total, p.Open, plural(p.Open, "issue", "issues")), Tone: tone}
}

func openReviewButton(sid string) sdui.Node {
	return sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+openReviewNode, "Open review", "checklist"), actOpenReview), sdui.VariantGhost), "cmd+r")
}

func (a *app) openReview(ctx context.Context, workspaceID, sid, id string) error {
	if id == "" {
		p := a.reviews.progress(sid)
		if p == nil {
			return nil
		}
		id = p.ID
	}
	return a.host.RunCommand(ctx, reviewShowCmd, workspaceID, reviewShowArgs{ID: id}, nil)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
