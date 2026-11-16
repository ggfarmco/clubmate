package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	landingReview     = "review…"
	landingReviewOf   = "review %d/%d…"
	landingAsking     = "asking"
	reviewAsksYou     = "asks you"
	reviewRoundLabel  = "Round %d review"
	reviewEarlier     = "Land review"
	landingIcon       = "shield"
	landingInto       = "Landing into %s"
	landingRoundOf    = "round %d of %d"
	landingOpenReview = "Open review"
	landingOpenKey    = "opt+r"
	landReviewNode    = "landReview"
	actLandReview     = "openLandReview"
	toneAccent        = "accent"
	tonePlain         = "plain"
	toneSuccess       = "success"
	toneWarning       = "warning"
)

type landingView struct {
	Round     int    `json:"round"`
	Of        int    `json:"of"`
	Verdict   string `json:"verdict"`
	Findings  int    `json:"findings,omitempty"`
	Asking    bool   `json:"asking,omitempty"`
	Words     string `json:"words"`
	Tone      string `json:"tone,omitempty"`
	Badge     string `json:"badge"`
	BadgeTone string `json:"badgeTone,omitempty"`
	Into      string `json:"into"`
	Source    string `json:"source,omitempty"`
	Reviewer  string `json:"reviewer,omitempty"`
}

func landingOf(st landReview, reviewing bool) (landingView, bool) {
	l := landingView{Into: st.Into, Source: st.Source, Reviewer: st.Reviewer}
	switch {
	case st.Request != "":
		l.Round, l.Verdict, l.Asking, l.Words, l.Tone = st.Rounds, gateChanges, true, landingAsking, toneWarning
	case reviewing:
		l.Round, l.Verdict, l.Tone = st.Rounds+1, gateStateReview, toneAccent
	case st.Last == gateChanges:
		l.Round, l.Verdict, l.Tone = st.Rounds, gateChanges, tonePlain
	case st.Last == gateApproved:
		l.Round, l.Verdict, l.Words, l.Tone = approvedRound(st), gateApproved, gateApproved, toneSuccess
	default:
		return landingView{}, false
	}
	l.Round = max(l.Round, 1)
	l.Of = max(st.Cap, l.Round)
	l.Badge, l.BadgeTone = verdictBadge(l.Verdict, len(st.Findings))
	if l.Verdict == gateChanges {
		l.Findings = len(st.Findings)
	}
	switch {
	case l.Verdict == gateStateReview && l.Round > 1:
		l.Words = fmt.Sprintf(landingReviewOf, l.Round, l.Of)
	case l.Verdict == gateStateReview:
		l.Words = landingReview
	case l.Verdict == gateChanges && !l.Asking:
		l.Words = l.Badge
	}
	return l, true
}

func approvedRound(st landReview) int {
	if st.Cap > 0 && st.Rounds >= st.Cap {
		return st.Rounds
	}
	return st.Rounds + 1
}

func verdictBadge(verdict string, findings int) (string, string) {
	switch verdict {
	case gateStateReview:
		return gateStateReview, toneAccent
	case gateChanges:
		if findings == 0 {
			return gateChanges, ""
		}
		return counted(findings, "change"), ""
	case gateApproved:
		return gateApproved, toneSuccess
	}
	return "", ""
}

func landingRank(l landingView) int {
	if l.Asking {
		return gateRank[gateStateNeedsMe]
	}
	return gateRank[l.Verdict]
}

func (a *app) landing(rec record) *landingView {
	if rec.closed() {
		return nil
	}
	var best *landingView
	for _, st := range rec.LandReviews {
		if !a.gateLive(st.Gate) {
			continue
		}
		_, reviewing := a.reviewing(rec.ID, st)
		l, ok := landingOf(st, reviewing)
		if s := a.session(l.Reviewer); s == nil || s.snapshot().Workspace != rec.Workspace {
			l.Reviewer = ""
		}
		if ok && (best == nil || landingRank(l) > landingRank(*best)) {
			best = &l
		}
	}
	return best
}

func reviewRound(rec record, st landReview) gateRound {
	r := gateRound{Round: st.Rounds, Verdict: st.Last}
	switch gf := rec.GateFor; {
	case gf != nil && !gf.Done && gf.Old != "":
		r.Round, r.Verdict = st.Rounds+1, gateStateReview
	case st.Last == gateApproved:
		r.Round = approvedRound(st)
	}
	r.Round = max(r.Round, 1)
	r.Label = fmt.Sprintf(reviewRoundLabel, r.Round)
	return r
}

func landerReview(lander record, rec record) (landReview, bool) {
	gf := rec.GateFor
	i := slices.IndexFunc(lander.LandReviews, func(st landReview) bool {
		return st.Into == gf.Into && st.Source == gf.Source && st.Reviewer == rec.ID
	})
	if i < 0 {
		return landReview{}, false
	}
	return lander.LandReviews[i], true
}

func earlierRound() gateRound {
	return gateRound{Label: reviewEarlier}
}

func reviewNests(recs []record) map[string]reviewerNest {
	byID := make(map[string]record, len(recs))
	for _, rec := range recs {
		byID[rec.ID] = rec
	}
	out := map[string]reviewerNest{}
	for _, rec := range recs {
		gf := rec.GateFor
		if gf == nil {
			continue
		}
		lander, listed := byID[gf.Lander]
		st, tracked := landerReview(lander, rec)
		tracked = listed && tracked
		n := reviewerNest{Lander: gf.Lander, Round: earlierRound(), Tracked: tracked, Asks: needsYou(rec) && !rec.closed(), Open: listed && !lander.closed()}
		if tracked {
			n.Round = reviewRound(rec, st)
		}
		n.Shown = n.Asks || !rec.closed() && (rec.Status == statusError || !tracked && rec.Run == nil)
		out[rec.ID] = n
	}
	return out
}

func reviewItem(item sdui.TreeItem, n reviewerNest) sdui.TreeItem {
	item.Label, item.Icon, item.Badges = n.Round.Label, landingIcon, nil
	if n.Asks {
		item.Tone, item.Detail, item.DetailTone, item.DetailFaint = toneWarning, reviewAsksYou, toneWarning, false
	}
	return item
}

func landingLine(sid string, l landingView) sdui.Node {
	parts := []sdui.Node{
		sdui.Icon(landingIcon),
		sdui.Text(fmt.Sprintf(landingInto, l.Into)),
		sdui.MutedText(fmt.Sprintf(landingRoundOf, l.Round, l.Of)),
		sdui.Badge(l.Badge, l.BadgeTone),
		sdui.Spacer(),
	}
	if l.Reviewer != "" {
		parts = append(parts, landReviewButton(sid+"/"+landReviewNode))
	}
	return sdui.WithID(sdui.HStack(parts...), sid+"/landing")
}

func landReviewButton(id string) sdui.Node {
	return sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(id, landingOpenReview, "checkmark.shield"), actLandReview), sdui.VariantGhost), landingOpenKey)
}

type reviewHead struct {
	Round  gateRound
	Lander string
	Title  string
	Into   string
	Source string
	Badge  string
	Tone   string
}

func (a *app) reviewHead(ctx context.Context, rec record) *reviewHead {
	gf := rec.GateFor
	if gf == nil {
		return nil
	}
	h := &reviewHead{Lander: gf.Lander, Into: gf.Into, Source: gf.Source}
	var st landReview
	tracked := false
	if s := a.session(gf.Lander); s != nil {
		lander := s.snapshot()
		h.Title = lander.Title
		st, tracked = landerReview(lander, rec)
	}
	findings := len(st.Findings)
	h.Round = reviewRound(rec, st)
	if !tracked {
		h.Round, findings = earlierRound(), 0
		if v, ok := a.loggedVerdict(ctx, rec); ok {
			h.Round = gateRound{Round: max(v.Round, 1), Verdict: v.Outcome}
			h.Round.Label = fmt.Sprintf(reviewRoundLabel, h.Round.Round)
			findings = len(v.Findings)
		}
	}
	if h.Round.Verdict != gateChanges {
		findings = 0
	}
	h.Badge, h.Tone = verdictBadge(h.Round.Verdict, findings)
	return h
}

func (a *app) loggedVerdict(ctx context.Context, rec record) (gateVerdict, bool) {
	gf := rec.GateFor
	if gf.Gate == "" || !gf.Done {
		return gateVerdict{}, false
	}
	history, err := a.readGateHistory(ctx, gf.Gate)
	if err != nil {
		return gateVerdict{}, false
	}
	i := slices.IndexFunc(history, func(v gateVerdict) bool { return v.Reviewer == rec.ID })
	if i < 0 {
		return gateVerdict{}, false
	}
	return history[i], true
}

func (h reviewHead) meta() []string {
	subject := landReview{Into: h.Into, Source: h.Source}.subject()
	return nonEmpty(ofLander(h.Title), subject)
}

func ofLander(title string) string {
	if title == "" {
		return ""
	}
	return "of " + title
}

func landingCount(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n) + " landing"
}

func (s *session) latestLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for _, en := range slices.Backward(s.entries) {
		if en.Kind == entryAgent && strings.TrimSpace(en.Text) != "" {
			line, _, _ := strings.Cut(strings.TrimSpace(en.Text), "\n")
			return line
		}
	}
	return ""
}

type reviewTrail struct {
	Reviewer string
	From     string
}

func reviewBack(rec record, trail reviewTrail) string {
	if trail.Reviewer == rec.ID && trail.From != "" {
		return trail.From
	}
	if rec.GateFor == nil {
		return ""
	}
	return rec.GateFor.Lander
}

func (a *app) closeReviewer(ctx context.Context, s *session) {
	switch rec := s.snapshot(); {
	case rec.closed():
	case rec.Status == statusError:
		a.closeTree(ctx, s)
	default:
		a.closeUnlessBusy(ctx, s, false, true)
	}
}

func (a *app) openReviewer(ctx context.Context, workspaceID, reviewer, from string) error {
	if _, err := a.lookup(workspaceID, reviewer); err != nil {
		return err
	}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.reviewFrom = reviewTrail{Reviewer: reviewer, From: from}
	a.uiMu.Unlock()
	if err := a.selectSession(ctx, workspaceID, reviewer); err != nil {
		return err
	}
	a.setSection(workspaceID, sectionID)
	return a.host.Focus(ctx, workspaceID, sectionID, mainSlot)
}

func (a *app) reviewCrumb(ctx context.Context, workspaceID string, rec record, h *reviewHead) string {
	if h == nil {
		return ""
	}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	trail := p.reviewFrom
	a.uiMu.Unlock()
	from := reviewBack(rec, trail)
	parts := []string{"Agents"}
	if gate, ok := strings.CutPrefix(from, gatePrefix); ok {
		parts = append(parts, "Land gates")
		gates, err := a.gates(ctx)
		if i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == gate }); err == nil && i >= 0 {
			parts = append(parts, gateName(gates[i]))
		}
	} else if s := a.session(from); s != nil {
		lander := s.snapshot()
		if branch := a.branches.name(ctx, workspaceID, lander.Worktree); branch != "" {
			parts = append(parts, branchLabel(branch))
		}
		parts = append(parts, cmp.Or(lander.Title, "New session"))
	} else {
		return ""
	}
	return strings.Join(append(parts, h.Round.Label), " / ")
}

func backBar(crumb string) sdui.Node {
	back := sdui.WithShortcut(sdui.WithAction(sdui.Button(backNode, "Back", "chevron.left"), actBack), "cmd+[")
	return sdui.HStack(back, sdui.MutedText(crumb))
}
