package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
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
	callRunLabel      = "Run anyway"
	callRunShort      = "run anyway"
	callAsk           = "Run it without another review?"
	callManyChanges   = "%d gates still ask for changes"
	callManyNeed      = "%d gates need you on this call"
	callRoundsLine    = "%s · %d of %s"
	callGroupHead     = "%s · %s"
	callRuleChanges   = "Run anyway runs the call as it is. One more round refuses it and sends the agent the findings; its next matching call is reviewed again. Stop cancels the call and stops the turn."
	callRuleRerun     = "Run anyway runs the call as it is. One more round runs the review again on this call. Stop cancels the call and stops the turn."
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
	r, h := a.findHeldRequest(workspaceID, request)
	if h == nil {
		return errLandStale
	}
	if r.point() == gatePointTool {
		return a.answerCallCard(ctx, r.s, request, action)
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
	if !r.cas(h, &n) {
		a.gateMu.Unlock()
		return errLandStale
	}
	if action == heldActionAccept && !h.Unread {
		v := landgate.Verdict{Tree: h.Key, Outcome: gateApproved, By: landgate.ByUser, At: time.Now().UTC()}
		if err := a.host.StorageSet(ctx, r.verdictPath(h.Gate, h.Key), v); err != nil {
			r.cas(&n, h)
			a.gateMu.Unlock()
			return err
		}
	}
	a.gateMu.Unlock()
	r.s.changed(changeStatus, true)
	switch {
	case action == heldActionAccept:
		a.releaseHold(r, n.Key, landgate.ByUser)
	case action == heldActionStop:
		a.cancel(r.s, true)
	case h.Unread:
		a.finishTurn(r.s)
	case changes:
		a.promptChanges(r, *g, n)
	default:
		a.reviewHeld(r)
	}
	return nil
}

func (a *app) findHeldRequest(workspaceID, request string) (holdRef, *heldTurn) {
	if request == "" {
		return holdRef{}, nil
	}
	for _, s := range a.workspaceSessions(workspaceID) {
		rec := s.snapshot()
		if rec.closed() {
			continue
		}
		for _, r := range s.holds() {
			if h := r.of(rec); h != nil && h.Request == request {
				return r, h
			}
		}
	}
	return holdRef{}, nil
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

func (r record) callRequests() []string {
	if r.closed() {
		return nil
	}
	var out []string
	for _, c := range r.Calls {
		if c.Hold.Request != "" && !slices.Contains(out, c.Hold.Request) {
			out = append(out, c.Hold.Request)
		}
	}
	return out
}

func callCard(rec record, request string) sdui.Node {
	holds := rec.cardHolds(request)
	rule := callRuleRerun
	if slices.ContainsFunc(holds, func(c *callHold) bool { return c.Hold.Last == gateChanges }) {
		rule = callRuleChanges
	}
	return roundsCard(rec, request, callRunLabel, gateMoreLabel, sdui.LandRequestProps{
		Title:    callCardTitle(holds),
		Caption:  callCaption(holds),
		Name:     rec.Title,
		Detail:   callDetail(holds),
		Rule:     rule,
		Question: callAsk,
	})
}

func callCardTitle(holds []*callHold) string {
	switch {
	case len(holds) == 0:
		return ""
	case len(holds) == 1:
		return heldCardTitle(*holds[0].Hold)
	case slices.ContainsFunc(holds, func(c *callHold) bool { return c.Hold.Last != gateChanges }):
		return fmt.Sprintf(callManyNeed, len(holds))
	}
	return fmt.Sprintf(callManyChanges, len(holds))
}

func callCaption(holds []*callHold) string {
	if len(holds) == 1 {
		return heldCaption(*holds[0].Hold)
	}
	parts := make([]string, 0, len(holds))
	for _, c := range holds {
		round := max(c.Hold.Round, 1)
		parts = append(parts, fmt.Sprintf(callRoundsLine, callMatch(*c.Hold), round, counted(max(c.Hold.Of, round), "round")))
	}
	return strings.Join(parts, " · ")
}

func callDetail(holds []*callHold) string {
	if len(holds) == 0 {
		return ""
	}
	call := holds[0].Input
	if i := slices.IndexFunc(holds, func(c *callHold) bool { return c.Call != "" }); i >= 0 {
		call = holds[i].Input
	}
	if len(holds) == 1 {
		return strings.Join(nonEmpty(call, heldDetail(*holds[0].Hold)), "\n\n")
	}
	parts := []string{call}
	for _, c := range holds {
		h := *c.Hold
		body := heldDetail(h)
		if h.Last != gateChanges {
			body = heldCardTitle(h)
		}
		parts = append(parts, strings.Join(nonEmpty(fmt.Sprintf(callGroupHead, callMatch(h), cmp.Or(h.Graph, heldGraphFallback)), body), "\n"))
	}
	return strings.Join(nonEmpty(parts...), "\n\n")
}

func callMatch(h heldTurn) string {
	return strings.TrimPrefix(h.Name, gateNameTool)
}
