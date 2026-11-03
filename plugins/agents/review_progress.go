package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"clubmate/pkg/sdui"
)

const (
	reviewTopic    = "editor-nvim.review"
	reviewShowCmd  = "editor-nvim.review-show"
	actOpenReview  = "openReview"
	openReviewNode = "openReview"
)

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
	Comments    []struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	} `json:"comments"`
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
	mu     sync.Mutex
	open   map[string]reviewPayload
	seq    map[string]int
	states map[string]map[int64]string
	next   int
}

type reviewShowArgs struct {
	ID string `json:"id"`
}

func newReviewBoard() *reviewBoard {
	return &reviewBoard{open: map[string]reviewPayload{}, seq: map[string]int{}, states: map[string]map[int64]string{}}
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
		delete(b.open, p.ID)
		delete(b.seq, p.ID)
		return p.WorkspaceID, people, true
	}
	b.next++
	b.open[p.ID], b.seq[p.ID] = p, b.next
	return p.WorkspaceID, people, true
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

func (b *reviewBoard) cards() (map[string]map[int64]string, map[string]bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	states := make(map[string]map[int64]string, len(b.states))
	for id, s := range b.states {
		states[id] = maps.Clone(s)
	}
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
