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
	reviewCardPrefix = openReviewNode + "/"
	commentOpen      = "open"
	commentWorking   = "working"
)

var reviewGlyphs = map[string]struct{ glyph, tone string }{
	"issue":      {"●", "warning"},
	"question":   {"?", "neutral"},
	"suggestion": {"◆", "neutral"},
}

type reviewComment struct {
	ID        int64  `json:"id"`
	File      string `json:"file"`
	Side      string `json:"side"`
	LineStart int    `json:"lineStart"`
	LineEnd   int    `json:"lineEnd"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Status    string `json:"status,omitempty"`
	LineText  string `json:"lineText,omitempty"`
}

type sendReviewArgs struct {
	Session  string          `json:"session"`
	Worktree string          `json:"worktree"`
	Review   string          `json:"review"`
	Base     string          `json:"base"`
	Head     string          `json:"head"`
	Reviewed int             `json:"reviewed"`
	Total    int             `json:"total"`
	Comments []reviewComment `json:"comments"`
	Note     string          `json:"note"`
	Nrev     string          `json:"nrev"`
}

type sendReviewResult struct {
	Session string `json:"session"`
}

type reviewSent struct {
	ID       string          `json:"id"`
	Worktree string          `json:"worktree"`
	Base     string          `json:"base"`
	Head     string          `json:"head"`
	Reviewed int             `json:"reviewed"`
	Total    int             `json:"total"`
	Comments []reviewComment `json:"comments"`
	Note     string          `json:"note,omitempty"`
}

func (a *app) sendReviewCommand(ctx context.Context, cmd pluginsdk.CommandRun, args sendReviewArgs) (any, error) {
	if cmd.Agent || cmd.Via != editorPlugin {
		return nil, errReviewOnly
	}
	if args.Worktree == "" || len(args.Comments) == 0 {
		return nil, errBadReview
	}
	sent := &reviewSent{ID: args.Review, Worktree: args.Worktree, Base: args.Base, Head: args.Head, Reviewed: args.Reviewed, Total: args.Total, Comments: args.Comments, Note: strings.TrimSpace(args.Note)}
	text := reviewPrompt(args)
	if sid := a.reviewRecipient(cmd.WorkspaceID, args.Session, args.Worktree); sid != "" {
		if err := a.prompt(cmd.WorkspaceID, promptArgs{Session: sid, Text: text, Review: sent}); err != nil {
			return nil, err
		}
		a.kick(cmd.WorkspaceID)
		return sendReviewResult{Session: sid}, nil
	}
	res, err := a.newSession(ctx, a.host, cmd.WorkspaceID, "", newArgs{Prompt: text, worktree: args.Worktree, review: sent})
	if err != nil {
		return nil, err
	}
	a.kick(cmd.WorkspaceID)
	return sendReviewResult{Session: res.Session}, nil
}

func (a *app) reviewRecipient(workspaceID, session, worktree string) string {
	usable := func(rec record) bool { return rec.Worktree == worktree && !rec.closed() && !rec.restricted() }
	if s, err := a.lookup(workspaceID, session); err == nil && usable(s.snapshot()) {
		return session
	}
	var best record
	for _, rec := range a.list(workspaceID) {
		if usable(rec) && rec.UpdatedAt.After(best.UpdatedAt) {
			best = rec
		}
	}
	return best.ID
}

func reviewPrompt(args sendReviewArgs) string {
	n := len(args.Comments)
	var b strings.Builder
	fmt.Fprintf(&b, "Review of %s against %s: %d %s from the Clubmate review panel.\n\n", args.Head, args.Base, n, plural(n, "comment", "comments"))
	fmt.Fprintf(&b, "For each one: fix it if you agree and resolve it with `%s resolve <id> --note \"<what you did>\"`; if it is for someone else, don't fix it and say who should handle it. End with one line per comment: <id> fixed | forwarded | disagree — why.", args.Nrev)
	for _, c := range args.Comments {
		fmt.Fprintf(&b, "\n\n%d. [%s] %s (%s)", c.ID, c.Type, commentPlace(c, true), c.Side)
		if c.LineText != "" {
			b.WriteString("\n   > " + c.LineText)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(c.Text), "\n") {
			b.WriteString("\n   " + line)
		}
	}
	if note := strings.TrimSpace(args.Note); note != "" {
		b.WriteString("\n\nNote: " + note)
	}
	return b.String()
}

func commentPlace(c reviewComment, span bool) string {
	place := fmt.Sprintf("%s:%d", c.File, c.LineStart)
	if span && c.LineEnd > c.LineStart {
		place += fmt.Sprintf("-%d", c.LineEnd)
	}
	return place
}

func reviewCard(sc screen, en *entry) sdui.Node {
	sid, r := sc.View.Session.ID, en.Review
	n := len(r.Comments)
	meta := fmt.Sprintf("you · %d/%d files · %d %s", r.Reviewed, r.Total, n, plural(n, "comment", "comments"))
	if !en.At.IsZero() {
		meta += " · " + en.At.Local().Format("15:04")
	}
	head := []sdui.Node{
		{Type: "text", Props: map[string]any{"text": "Review", "style": "title"}},
		sdui.MutedText(meta),
	}
	if sc.ReviewsOpen[r.ID] {
		open := sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+reviewCardPrefix+en.ID, "Open review", "checklist"), actOpenReview), sdui.VariantGhost)
		if sc.Review != nil && sc.Review.ID == r.ID {
			open = sdui.WithShortcut(open, "cmd+r")
		}
		head = append(head, sdui.Spacer(), open)
	}
	children := []sdui.Node{sdui.HStack(head...)}
	states := sc.ReviewStates[r.ID]
	for _, c := range r.Comments {
		g, ok := reviewGlyphs[c.Type]
		if !ok {
			g.glyph, g.tone = "○", "neutral"
		}
		row := sdui.HStack(sdui.WithTone(sdui.Text(g.glyph), g.tone), sdui.MutedText(commentPlace(c, false)), sdui.Text(firstLine(c.Text)), sdui.Spacer(), sdui.MutedText(commentState(states[c.ID], sc.View.Session.Status)))
		row.ID = fmt.Sprintf("%s/review/%s/c%d", sid, en.ID, c.ID)
		children = append(children, row)
	}
	if r.Note != "" {
		children = append(children, sdui.Text("Also: "+r.Note))
	}
	card := sdui.Card("", "person", children...)
	card.ID = sid + "/msg/" + en.Kind + "/" + en.ID
	return card
}

func commentState(state, status string) string {
	switch {
	case state != "" && state != commentOpen:
		return state
	case status == statusRunning:
		return commentWorking
	default:
		return commentOpen
	}
}

func reviewCardKey(sc screen, en *entry) string {
	if en.Review == nil {
		return ""
	}
	states := sc.ReviewStates[en.Review.ID]
	current := sc.Review != nil && sc.Review.ID == en.Review.ID
	parts := []string{fmt.Sprint(sc.ReviewsOpen[en.Review.ID], current), sc.View.Session.Status}
	for _, c := range en.Review.Comments {
		parts = append(parts, states[c.ID])
	}
	return strings.Join(parts, "\x00")
}

func (a *app) cardReview(s *session, node string) string {
	id, ok := strings.CutPrefix(node, reviewCardPrefix)
	if !ok {
		return ""
	}
	v := s.view(0)
	i := slices.IndexFunc(v.Entries, func(en *entry) bool { return en.ID == id && en.Review != nil })
	if i < 0 {
		return ""
	}
	return v.Entries[i].Review.ID
}
