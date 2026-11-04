package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"clubmate/pkg/sdui"
)

const (
	sendReviewCmd   = "agents.send-review"
	sessionsCmd     = "agents.sessions"
	sendSheetID     = "review/sendSheet"
	sendToNode      = sendSheetID + "/to"
	sendNoteNode    = sendSheetID + "/note"
	newRecipient    = "new"
	exportTimeout   = 2 * time.Second
	statusClosed    = "closed"
	recipientAgent  = "Claude"
	newSessionTitle = "a new session"
	sendToOpener    = "To the session that made these changes:"
	sendToOther     = "To"
)

var commentGlyphs = map[string][2]string{
	"issue":      {"●", "warning"},
	"question":   {"?", "neutral"},
	"suggestion": {"◆", "neutral"},
	"":           {"○", "neutral"},
}

var errNotRecipient = errors.New("send review: not one of the offered recipients")

type sendSheet struct {
	Gen     int64
	Options []sdui.Option
	Default string
	Titles  map[string]string
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

func (a *app) sendAt(ctx context.Context, h reviewHost, ws, root string, tab int) error {
	a.reviewMu.Lock()
	r := a.reviewAtLocked(ws, root, tab)
	a.reviewMu.Unlock()
	if r == nil {
		return errUnknownReview
	}
	return a.openSendSheet(ctx, h, r.ID)
}

func (a *app) openSendSheet(ctx context.Context, h reviewHost, id string) error {
	sheet, err := a.offerRecipients(ctx, h, id)
	if err != nil {
		return err
	}
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	r := a.reviews[id]
	if r == nil {
		return errUnknownReview
	}
	a.adoptRecipientsLocked(r, sheet)
	if len(r.openComments()) == 0 {
		return nil
	}
	for _, o := range a.workspaceReviewsLocked(r.Workspace) {
		o.Sheet = nil
	}
	a.sheetSeq = max(a.sheetSeq+1, time.Now().UnixNano())
	sheet.Gen = a.sheetSeq
	r.Sheet = sheet
	a.reviewSel[r.Workspace] = r.ID
	a.renderReviewsLocked(ctx, h, r.Workspace)
	return nil
}

func (a *app) refreshMeta(ctx context.Context, h reviewHost, id string) {
	sheet, err := a.offerRecipients(ctx, h, id)
	if err != nil {
		return
	}
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	if r := a.reviews[id]; r != nil {
		a.adoptRecipientsLocked(r, sheet)
	}
}

func (a *app) offerRecipients(ctx context.Context, h reviewHost, id string) (*sendSheet, error) {
	a.reviewMu.Lock()
	r := a.reviews[id]
	if r == nil {
		a.reviewMu.Unlock()
		return nil, errUnknownReview
	}
	ws, worktree, opener := r.Workspace, r.Key.Worktree, r.Opener
	a.reviewMu.Unlock()
	var list []agentSession
	if err := h.RunCommand(ctx, sessionsCmd, ws, map[string]string{}, &list); err != nil {
		logError("review recipients", err)
	}
	return recipients(list, worktree, opener), nil
}

func (a *app) adoptRecipientsLocked(r *review, sheet *sendSheet) {
	r.DefaultTitle = cmp.Or(sheet.Titles[sheet.Default], newSessionTitle)
	r.sendMetaLocked()
}

func (r *review) meta() reviewMeta {
	subtitle := r.Info.Labels.Right + " ↔ " + r.Info.Labels.Left
	if r.OpenerTitle != "" {
		subtitle = recipientAgent + " · " + r.OpenerTitle
	}
	return reviewMeta{Subtitle: subtitle, Recipient: r.DefaultTitle, Sent: r.Sent}
}

func (r *review) sendMetaLocked() {
	m := r.meta()
	if r.metaSent != nil && *r.metaSent == m {
		return
	}
	if err := r.nv.reviewMeta(r.Tab, m); err != nil {
		logError("review meta", err)
		return
	}
	r.metaSent = &m
}

func recipients(list []agentSession, worktree, opener string) *sendSheet {
	open := slices.DeleteFunc(slices.Clone(list), func(s agentSession) bool {
		return s.Worktree != worktree || s.Status == statusClosed || s.Service
	})
	slices.SortStableFunc(open, func(x, y agentSession) int {
		if (x.ID == opener) != (y.ID == opener) {
			if x.ID == opener {
				return -1
			}
			return 1
		}
		return y.UpdatedAt.Compare(x.UpdatedAt)
	})
	sheet := &sendSheet{Default: newRecipient, Titles: map[string]string{}}
	for _, s := range open {
		title := cmp.Or(s.Title, "New session")
		sheet.Titles[s.ID] = title
		sheet.Options = append(sheet.Options, sdui.Option{Value: s.ID, Label: title + " · " + s.Status})
	}
	if len(open) > 0 {
		sheet.Default = open[0].ID
	}
	sheet.Options = append(sheet.Options, sdui.Option{Value: newRecipient, Label: "New session in " + filepath.Base(worktree)})
	return sheet
}

func (a *app) submitSend(ctx context.Context, h reviewHost, ws string, raw json.RawMessage) error {
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("send review: %w", err)
	}
	a.reviewMu.Lock()
	r := a.reviews[a.reviewSel[ws]]
	if r == nil || r.Workspace != ws || r.Sheet == nil {
		a.reviewMu.Unlock()
		return errUnknownReview
	}
	to := cmp.Or(values[sendToNode], r.Sheet.Default)
	title, offered := r.Sheet.Titles[to]
	if !offered && to != newRecipient {
		a.reviewMu.Unlock()
		return fmt.Errorf("%w: %s", errNotRecipient, to)
	}
	done, total := r.progress()
	args := sendReviewArgs{
		Worktree: r.Key.Worktree, Review: r.ID, Base: r.Info.Labels.Left, Head: r.Info.Labels.Right,
		Reviewed: done, Total: total, Comments: r.openComments(), Note: values[sendNoteNode], Nrev: a.nrev,
	}
	if to != newRecipient {
		args.Session = to
	}
	id := r.ID
	a.reviewMu.Unlock()
	if len(args.Comments) == 0 {
		return a.closeSendSheet(ctx, h, ws)
	}
	var res sendReviewResult
	if err := h.RunCommand(ctx, sendReviewCmd, ws, args, &res); err != nil {
		return err
	}
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	if r = a.reviews[id]; r == nil {
		return nil
	}
	r.Recipient, r.RecipientTitle, r.Sent, r.Sheet = res.Session, cmp.Or(title, newSessionTitle), len(args.Comments), nil
	r.sendMetaLocked()
	a.persistReview(ctx, h, r)
	a.reviewChangedLocked(ctx, h, r)
	return nil
}

func (a *app) closeSendSheet(ctx context.Context, h reviewHost, ws string) error {
	a.reviewMu.Lock()
	defer a.reviewMu.Unlock()
	if r := a.reviews[a.reviewSel[ws]]; r != nil && r.Sheet != nil {
		r.Sheet = nil
		a.renderReviewsLocked(ctx, h, ws)
	}
	return nil
}

func sendSheetView(r *review) sdui.Node {
	s := r.Sheet
	id := fmt.Sprintf("%s/%d", sendSheetID, s.Gen)
	open := r.openComments()
	to := sdui.HStack(sdui.MutedText(sendToOther), sdui.Select(sendToNode, s.Options, s.Default))
	if r.Opener != "" && s.Default == r.Opener {
		to = sdui.HStack(sdui.Text(sendToOpener), sdui.Select(sendToNode, s.Options, s.Default))
	}
	children := []sdui.Node{
		sdui.HStack(sdui.WithTone(sdui.Icon("bubble.left"), "accent"), textStyled("Send review to "+recipientAgent+"?", "title")),
		to,
	}
	for _, c := range open {
		g, ok := commentGlyphs[c.Type]
		if !ok {
			g = commentGlyphs[""]
		}
		children = append(children, sdui.VStack(
			sdui.HStack(sdui.WithTone(sdui.Text(g[0]), g[1]), sdui.MutedText(fmt.Sprintf("%s · %s:%d", cmp.Or(c.Type, "comment"), c.File, c.LineStart))),
			sdui.Text(c.Text),
		))
	}
	children = append(children,
		sdui.TextField(sendNoteNode, "", "Anything else for "+recipientAgent+"? (optional)"),
		sdui.MutedText("Resolved comments stay as notes and aren't sent. Files "+recipientAgent+" changes again turn ● here."),
		sdui.HStack(
			sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/cancel", "Cancel", ""), "cancel"), sdui.VariantSecondary),
			sdui.WithShortcut(sdui.WithVariant(sdui.Button(id+"/send", fmt.Sprintf("Send %d %s", len(open), plural(len(open), "comment", "comments")), "bubble.left"), sdui.VariantPrimary), "cmd+return"),
		),
	)
	form := sdui.WithAction(sdui.Form(id, children...), "sendReview")
	form.Props["sheet"] = true
	return form
}

func (a *app) sendFromStatus(ctx context.Context, h reviewHost, ws string) error {
	for _, e := range a.workspaceEditors(ws) {
		if a.isCurrent(e) {
			e.mu.Lock()
			tab := e.status.Tab
			e.mu.Unlock()
			return a.sendAt(ctx, h, ws, e.root, tab)
		}
	}
	return errUnknownReview
}
