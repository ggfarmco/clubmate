package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/landgate"
)

const (
	heldActionAccept  = "accept"
	heldActionStop    = "stop"
	heldAcceptLabel   = "Accept as done"
	heldGraphFallback = "The review"
	heldChangesTitle  = "%s still asks for changes after %s"
	heldNoVerdict     = "%s ended without a verdict"
	heldKeptChanging  = "The files kept changing during %s"
	heldSinceFormat   = "held since %s"
	heldSummaryFormat = "Summary: %s"
	heldRuleChanges   = "Accept as done shows the turn as done as it is. One more round sends the agent these findings and reviews its next finished turn. Stop leaves the session idle and its turn not done."
	heldRuleRerun     = "Accept as done shows the turn as done as it is. One more round runs the review again on this turn. Stop leaves the session idle and its turn not done."
	heldUnreadTitle   = "Couldn't check this turn against its gates"
	heldUnreadAsk     = "Accept as done without a review?"
	heldUnreadRule    = "When the turn ended, Clubmate couldn't read %s%s. It tried %s. Check again tries now. Stop leaves the session idle and the turn not done."
	heldUnreadGates   = "the gates"
	heldUnreadRepo    = "%s's repo"
	heldTriedFormat   = "tried %s"
	heldCheckLabel    = "Check again"
	heldCheckShort    = "check again"
)

var errGateAction = errors.New("action is accept, more or stop")

type gateAnswerArgs struct {
	Request string `json:"request"`
	Action  string `json:"action"`
}

func (a *app) gateAnswerCommand(ctx context.Context, cmd pluginsdk.CommandRun, args gateAnswerArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errGateUser
	}
	return nil, a.answerHeld(ctx, cmd.WorkspaceID, args.Request, args.Action)
}

func (a *app) answerHeld(ctx context.Context, workspaceID, request, action string) error {
	s, h := a.findHeldRequest(workspaceID, request)
	if s == nil {
		return errLandStale
	}
	n := *h
	n.Request, n.Asked = "", time.Time{}
	changes := h.Last == gateChanges
	var g *gateConfig
	switch action {
	case heldActionAccept, heldActionStop:
	case landActionMore:
		if h.Unread {
			break
		}
		if g = a.gateByID(ctx, h.Gate); g == nil {
			return errLandStale
		}
		if changes {
			n.Extra++
		} else {
			n.Retried, n.Moves = false, 0
		}
	default:
		return fmt.Errorf("%w: %s", errGateAction, action)
	}
	a.gateMu.Lock()
	if !s.casHeld(h, &n) {
		a.gateMu.Unlock()
		return errLandStale
	}
	if action == heldActionAccept && !h.Unread {
		v := landgate.Verdict{Tree: h.Key, Outcome: gateApproved, By: landgate.ByUser, At: time.Now().UTC()}
		if err := a.host.StorageSet(ctx, turnVerdictPath(h.Gate, h.Key), v); err != nil {
			s.casHeld(&n, h)
			a.gateMu.Unlock()
			return err
		}
	}
	a.gateMu.Unlock()
	s.changed(changeStatus, true)
	switch {
	case action == heldActionAccept:
		a.releaseHeld(s, n.Key, landgate.ByUser)
	case action == heldActionStop:
		a.cancel(s)
	case h.Unread:
		a.finishTurn(s)
	case changes:
		a.promptChanges(s, *g, n)
	default:
		a.reviewHeld(s)
	}
	return nil
}

func (a *app) findHeldRequest(workspaceID, request string) (*session, *heldTurn) {
	if request == "" {
		return nil, nil
	}
	for _, s := range a.workspaceSessions(workspaceID) {
		if rec := s.snapshot(); !rec.closed() && rec.Held != nil && rec.Held.Request == request {
			return s, rec.Held
		}
	}
	return nil, nil
}

func heldAction(landAction string) string {
	switch landAction {
	case landActionMerge:
		return heldActionAccept
	case landActionLeave:
		return heldActionStop
	}
	return landAction
}

func heldCard(rec record, h heldTurn) sdui.Node {
	if h.Unread {
		return unreadCard(rec, h)
	}
	rule := heldRuleRerun
	if h.Last == gateChanges {
		rule = heldRuleChanges
	}
	return roundsCard(rec, h.Request, heldAcceptLabel, gateMoreLabel, sdui.LandRequestProps{
		Title:    heldCardTitle(h),
		Caption:  heldCaption(h),
		Name:     cmp.Or(rec.Title, h.Name),
		Detail:   heldDetail(h),
		Rule:     rule,
		Question: heldQuestion(h),
	})
}

func unreadCard(rec record, h heldTurn) sdui.Node {
	what := heldUnreadGates
	if h.ReadRepo != "" {
		what = fmt.Sprintf(heldUnreadRepo, h.ReadRepo)
	}
	reason := ""
	if h.Reason != "" {
		reason = ": " + h.Reason
	}
	tried := make([]string, 0, len(h.Tried))
	for _, at := range h.Tried {
		tried = append(tried, clockOf(at))
	}
	caption := heldCaption(heldTurn{Since: h.Since})
	if len(tried) > 0 {
		caption = strings.Join(nonEmpty(caption, fmt.Sprintf(heldTriedFormat, strings.Join(tried, ", "))), " · ")
	}
	return roundsCard(rec, h.Request, heldAcceptLabel, heldCheckLabel, sdui.LandRequestProps{
		Title:    heldCardTitle(h),
		Caption:  caption,
		Name:     cmp.Or(rec.Title, h.Name),
		Rule:     fmt.Sprintf(heldUnreadRule, what, reason, counted(len(h.Tried), "time")),
		Question: heldQuestion(h),
	})
}

func heldCardTitle(h heldTurn) string {
	graph := cmp.Or(h.Graph, heldGraphFallback)
	if h.Unread {
		return heldUnreadTitle
	}
	if h.Last == gateChanges {
		return fmt.Sprintf(heldChangesTitle, graph, counted(max(h.Round, 1), "round"))
	}
	if h.Moves >= heldMovesMax {
		return fmt.Sprintf(heldKeptChanging, graph)
	}
	return fmt.Sprintf(heldNoVerdict, graph)
}

func heldCaption(h heldTurn) string {
	since := ""
	if at := clockOf(h.Since); at != "" {
		since = fmt.Sprintf(heldSinceFormat, at)
	}
	return strings.Join(nonEmpty(h.Repo, h.Name, since), " · ")
}

func heldDetail(h heldTurn) string {
	parts := nonEmpty(bullets(h.Findings))
	if h.Summary != "" {
		parts = append(parts, fmt.Sprintf(heldSummaryFormat, h.Summary))
	}
	return strings.Join(parts, "\n\n")
}

func (r record) heldWaits() bool {
	return !r.closed() && r.Held != nil && r.Held.Request != ""
}
